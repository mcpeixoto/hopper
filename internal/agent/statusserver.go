package agent

import (
	"encoding/json"
	"net/http"

	nodeui "github.com/mcpeixoto/hopper/web/node"
)

// StatusHandler serves the agent's live status JSON plus the embedded node console
// at "/". It is intended to be bound to a loopback address only.
func (a *Agent) StatusHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*") // loopback-only dev convenience
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a.Status())
	})
	// Embedded node GUI (index.html, app.js, styles.css) served at root.
	mux.Handle("GET /", http.FileServerFS(nodeui.FS()))
	return mux
}
