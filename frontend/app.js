// app.js
document.addEventListener("DOMContentLoaded", () => {
  const batchInput = document.getElementById("batchInput");
  const updateBtn  = document.getElementById("updateBtn");
  const countEl    = document.getElementById("count");
  const lastEl     = document.getElementById("last");

  // Fetch & render current batchSize
  async function loadConfig() {
    try {
      const res = await fetch("/api/config");
      const { batchSize } = await res.json();
      batchInput.value = batchSize;
    } catch (e) {
      console.error("Failed to load config:", e);
    }
  }

  // Fetch & render health stats
  async function loadHealth() {
    try {
      const res = await fetch("/api/health");
      const { processedCount, lastProcessed } = await res.json();
      countEl.textContent = processedCount;
      lastEl.textContent   = new Date(lastProcessed).toLocaleString();
    } catch (e) {
      console.error("Failed to load health:", e);
    }
  }

  // Handle the update-batch button click
  updateBtn.addEventListener("click", async () => {
    const newSize = parseInt(batchInput.value, 10);
    if (isNaN(newSize) || newSize < 1) {
      alert("Please enter a valid number ≥ 1");
      return;
    }

    // UI feedback
    updateBtn.disabled    = true;
    updateBtn.textContent = "Updating…";

    try {
      const res = await fetch("/api/config", {
        method:  "POST",
        headers: { "Content-Type": "application/json" },
        body:    JSON.stringify({ batchSize: newSize })
      });
      if (!res.ok) throw new Error(res.statusText);
      updateBtn.textContent = "Updated ✔";
    } catch (e) {
      console.error("Update failed:", e);
      updateBtn.textContent = "Failed ✖";
    }

    // Reset button after a moment
    setTimeout(() => {
      updateBtn.disabled    = false;
      updateBtn.textContent = "Update Batch Size";
    }, 1500);
  });

  // Initial loads
  loadConfig();
  loadHealth();
  // Poll health every 5 s
  setInterval(loadHealth, 2000);
});
