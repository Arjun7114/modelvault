// Package api exposes the modelvault engine over HTTP.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/Arjun7114/modelvault/internal/engine"
)

// Server wraps an engine and serves it over HTTP.
type Server struct {
	engine *engine.Engine
}

// NewServer returns a Server backed by the given engine.
func NewServer(e *engine.Engine) *Server {
	return &Server{engine: e}
}

// Routes registers all endpoints and returns the handler. It uses the Go 1.22+
// ServeMux method+path patterns, so no third-party router is needed.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/backup", s.handleBackup)
	mux.HandleFunc("GET /v1/snapshots", s.handleList)
	mux.HandleFunc("GET /v1/restore/{id}", s.handleRestore)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleBackup backs up the raw request body. ?source=NAME labels the snapshot.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "upload"
	}
	snap, stats, err := s.engine.Backup(r.Context(), source, r.Body)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"snapshot": snap, "stats": stats})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	ids, err := s.engine.ListSnapshots(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": ids})
}

// handleRestore streams the reconstructed bytes back to the client.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	w.Header().Set("Content-Type", "application/octet-stream")
	// NOTE: Restore streams directly to the response. The common failure
	// (unknown id) happens before any bytes are written, so the error below is
	// sent cleanly; a mid-stream failure can only truncate an already-200 body.
	if _, err := s.engine.Restore(r.Context(), id, w); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}