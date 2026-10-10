// Package api exposes the modelvault engine over HTTP.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/Arjun7114/modelvault/internal/engine"
)

// Server wraps an engine and serves it over HTTP.
type Server struct {
	engine *engine.Engine
	logger *slog.Logger
}

// NewServer returns a Server backed by the given engine. A nil logger falls
// back to slog.Default().
func NewServer(e *engine.Engine, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{engine: e, logger: logger}
}

// Routes registers all endpoints and wraps them in request logging.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/backup", s.handleBackup)
	mux.HandleFunc("GET /v1/snapshots", s.handleList)
	mux.HandleFunc("GET /v1/restore/{id}", s.handleRestore)
	return s.logging(mux)
}

// logging wraps a handler, emitting one structured log line per request.
func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// statusRecorder captures the status code and byte count for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "upload"
	}
	snap, stats, err := s.engine.Backup(r.Context(), source, r.Body)
	if err != nil {
		s.logger.Error("backup failed", "source", source, "err", err)
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"snapshot": snap, "stats": stats})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	ids, err := s.engine.ListSnapshots(r.Context())
	if err != nil {
		s.logger.Error("list failed", "err", err)
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": ids})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := s.engine.Restore(r.Context(), id, w); err != nil {
		s.logger.Error("restore failed", "id", id, "err", err)
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