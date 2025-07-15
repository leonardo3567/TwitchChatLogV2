package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
)

// —————————————————————————————————————————
// Configure these values for your setup
// —————————————————————————————————————————
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

// —————————————————————————————————————————

func main() {
	// 1) Connect to SQL Server
	db, err := sql.Open("sqlserver", connString)
	if err != nil {
		panic(fmt.Errorf("sql.Open error: %w", err))
	}
	defer db.Close()

	// Verify connection
	if err = db.Ping(); err != nil {
		panic(fmt.Errorf("db.Ping error: %w", err))
	}
	fmt.Println("✅ Connected to SQL Server")

	// 2) Create table if it doesn’t exist
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

	// 3) Prepare insert statement
	stmt, err := db.Prepare(`
    INSERT INTO dbo.Messages (Username, ChatText, ChatTime)
    VALUES (@p1, @p2, @p3)
  `)
	if err != nil {
		panic(fmt.Errorf("prepare insert: %w", err))
	}
	defer stmt.Close()

	// 4) Connect to Twitch IRC
	conn, err := net.Dial("tcp", "irc.chat.twitch.tv:6667")
	if err != nil {
		panic(fmt.Errorf("dial IRC: %w", err))
	}
	defer conn.Close()
	fmt.Println("✅ Connected to Twitch IRC")

	// Authenticate & join channel
	fmt.Fprintf(conn, "PASS %s\r\n", oauth)
	fmt.Fprintf(conn, "NICK %s\r\n", nickname)
	fmt.Fprintf(conn, "JOIN #%s\r\n", channel)

	reader := bufio.NewReader(conn)

	// 5) Read loop
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Read error:", err)
			break
		}
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "PING") {
			fmt.Fprintf(conn, "PONG :tmi.twitch.tv\r\n")
			continue
		}
		if !strings.Contains(line, "PRIVMSG") {
			continue
		}

		// split into “meta” and “text”
		parts := strings.SplitN(line, " :", 2)
		if len(parts) < 2 {
			continue
		}
		meta := parts[0] // ":username!username@.... PRIVMSG #chan"
		text := parts[1] // "the actual chat message"

		// extract the raw user token, then split on "!" and take [0]
		userToken := strings.Split(strings.Split(meta, " ")[0], "!")[0]
		username := strings.TrimPrefix(userToken, ":")

		// now username == "gomes3567" instead of "gomes3567!..."
		if _, err := stmt.Exec(username, text, time.Now()); err != nil {
			fmt.Println("DB insert error:", err)
		}
	}

}
