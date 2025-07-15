// main.go
package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
)

const (
	// Twitch IRC credentials
	oauth    = "oauth:w6u9na8pejq46btedmwia86zadhzy9"
	nickname = "gomes3567"
	channel  = "quin69"

	// SQL Server connection string
	// Note: use =, and match the name below in sql.Open
	connString = "server=127.0.0.1;" + // localhost IPv4
		"port=1433;" + // default SQL port
		"user id=sa2;" + // your login
		"password=35671779;" + // your password
		"database=TwitchChatDB;" + // target DB
		"encrypt=disable;" + // no TLS
		"trustservercertificate=true" // skip cert checks
)

// batchSize controls how many messages to buffer before flushing
var batchSize = 5

// msgQueue receives incoming chat messages
var msgQueue = make(chan Message, 1000)

// healthMetrics tracks total processed and last flush time
var healthMetrics = struct {
	processedCount int
	lastProcessed  time.Time
	mu             sync.RWMutex
}{}

type Message struct {
	Username string
	ChatText string
	ChatTime time.Time
}

func main() {
	// 1) Connect to SQL Server
	db, err := sql.Open("sqlserver", connString)
	if err != nil {
		panic(fmt.Errorf("sql.Open error: %w", err))
	}
	defer db.Close()

	// verify connection
	if err = db.Ping(); err != nil {
		panic(fmt.Errorf("db.Ping error: %w", err))
	}
	fmt.Println("✅ Connected to SQL Server")

	// 2) Ensure messages table exists
	createTbl := `
  IF NOT EXISTS (
    SELECT 1 FROM sys.tables WHERE name = 'Messages'
  )
  BEGIN
    CREATE TABLE dbo.Messages (
      ID        INT         IDENTITY(1,1) PRIMARY KEY,
      Username  NVARCHAR(100) NOT NULL,
      ChatText  NVARCHAR(MAX) NOT NULL,
      ChatTime  DATETIME2     NOT NULL
    );
  END
  `
	if _, err = db.Exec(createTbl); err != nil {
		panic(fmt.Errorf("creating table: %w", err))
	}
	fmt.Println("✅ Ensured table dbo.Messages exists")

	// 3) Start batch processor
	go startBatchProcessor(db)

	// 4) HTTP endpoints + static frontend
	http.Handle("/", http.FileServer(http.Dir("./frontend")))
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/health", handleHealth)

	go func() {
		fmt.Println("🔸 HTTP server running on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			panic(fmt.Errorf("HTTP server error: %w", err))
		}
	}()

	// 5) Connect to Twitch IRC
	conn, err := net.Dial("tcp", "irc.chat.twitch.tv:6667")
	if err != nil {
		panic(fmt.Errorf("dial IRC: %w", err))
	}
	defer conn.Close()
	fmt.Println("✅ Connected to Twitch IRC")

	fmt.Fprintf(conn, "PASS %s\r\n", oauth)
	fmt.Fprintf(conn, "NICK %s\r\n", nickname)
	fmt.Fprintf(conn, "JOIN #%s\r\n", channel)

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Read error:", err)
			break
		}
		line = strings.TrimSpace(line)

		// PING/PONG to keep alive
		if strings.HasPrefix(line, "PING") {
			fmt.Fprintf(conn, "PONG :tmi.twitch.tv\r\n")
			continue
		}
		if !strings.Contains(line, "PRIVMSG") {
			continue
		}

		parts := strings.SplitN(line, " :", 2)
		if len(parts) < 2 {
			continue
		}
		meta := parts[0]
		text := parts[1]
		userToken := strings.Split(strings.Split(meta, " ")[0], "!")[0]
		user := strings.TrimPrefix(userToken, ":")

		msgQueue <- Message{
			Username: user,
			ChatText: text,
			ChatTime: time.Now(),
		}
	}
}

func startBatchProcessor(db *sql.DB) {
	buffer := make([]Message, 0, batchSize)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-msgQueue:
			buffer = append(buffer, msg)
			if len(buffer) >= batchSize {
				flushBatch(db, buffer)
				buffer = buffer[:0]
			}

		case <-ticker.C:
			if len(buffer) > 0 {
				flushBatch(db, buffer)
				buffer = buffer[:0]
			}
		}
	}
}

func flushBatch(db *sql.DB, msgs []Message) {
	tx, err := db.Begin()
	if err != nil {
		fmt.Println("Begin transaction error:", err)
		return
	}
	stmt, err := tx.Prepare(`
    INSERT INTO dbo.Messages (Username, ChatText, ChatTime)
    VALUES (@p1, @p2, @p3)
  `)
	if err != nil {
		fmt.Println("Prepare insert error:", err)
		_ = tx.Rollback()
		return
	}
	defer stmt.Close()

	for _, m := range msgs {
		if _, err := stmt.Exec(m.Username, m.ChatText, m.ChatTime); err != nil {
			fmt.Println("Exec insert error:", err)
		}
	}
	if err := tx.Commit(); err != nil {
		fmt.Println("Commit transaction error:", err)
		return
	}

	// update health metrics
	healthMetrics.mu.Lock()
	healthMetrics.processedCount += len(msgs)
	healthMetrics.lastProcessed = time.Now()
	healthMetrics.mu.Unlock()
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(map[string]int{"batchSize": batchSize})

	case http.MethodPost:
		var payload struct{ BatchSize int }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if payload.BatchSize < 1 {
			http.Error(w, "batchSize must be ≥ 1", http.StatusBadRequest)
			return
		}
		batchSize = payload.BatchSize
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	healthMetrics.mu.RLock()
	defer healthMetrics.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"processedCount": healthMetrics.processedCount,
		"lastProcessed":  healthMetrics.lastProcessed.Format(time.RFC3339),
	})
}
