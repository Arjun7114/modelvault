// Package api exposes the modelvault engine over HTTP.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Arjun7114/modelvault/internal/engine"
)

// Server wraps an engine and serves it over HTTP.
type Server struct {
	engine   *engine.Engine
	logger   *slog.Logger
	requests metric.Int64Counter
	duration metric.Float64Histogram
}

// NewServer returns a Server backed by the given engine. A nil logger falls
// back to slog.Default(). Metric instruments are no-ops until a global
// MeterProvider is installed (see observability.InitMeter).
func NewServer(e *engine.Engine, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	meter := otel.Meter("modelvault/api")
	requests, _ := meter.Int64Counter(
		"modelvault.http.requests.total",
		metric.WithDescription("Total HTTP requests by route and status"),
	)
	duration, _ := meter.Float64Histogram(
		"modelvault.http.request.duration.seconds",
		metric.WithUnit("s"),
		metric.WithDescription("HTTP request duration in seconds"),
	)
	return &Server{engine: e, logger: logger, requests: requests, duration: duration}
}

// Routes registers all endpoints, exposes /metrics, and wraps the rest in
// logging + metrics instrumentation.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/backup", s.handleBackup)
	mux.HandleFunc("GET /v1/snapshots", s.handleList)
	mux.HandleFunc("GET /v1/restore/{id}", s.handleRestore)
	mux.Handle("GET /metrics", promhttp.Handler())
	return s.instrument(mux)
}

// instrument logs and records metrics for every request. Metric labels use the
// matched route pattern (r.Pattern), not the raw path, so the snapshot id in
// /v1/restore/{id} never becomes a high-cardinality label.
func (s *Server) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start)

		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}

		s.logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", elapsed.Milliseconds(),
			"remote", r.RemoteAddr,
		)

		s.requests.Add(r.Context(), 1, metric.WithAttributes(
			attribute.String("route", route),
			attribute.Int("status", rec.status),
		))
		s.duration.Record(r.Context(), elapsed.Seconds(), metric.WithAttributes(
			attribute.String("route", route),
		))
	})
}

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
	ctx, span := otel.Tracer("modelvault/api").Start(r.Context(), "backup")
	defer span.End()

	source := r.URL.Query().Get("source")
	if source == "" {
		source = "upload"
	}
	span.SetAttributes(attribute.String("source", source))

	snap, stats, err := s.engine.Backup(ctx, source, r.Body)
	if err != nil {
		span.RecordError(err)
		s.logger.Error("backup failed", "source", source, "err", err)
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	span.SetAttributes(
		attribute.Int("chunks.total", stats.TotalChunks),
		attribute.Int("chunks.new", stats.NewChunks),
		attribute.Int64("bytes.total", stats.TotalBytes),
	)
	writeJSON(w, http.StatusCreated, map[string]any{"snapshot": snap, "stats": stats})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("modelvault/api").Start(r.Context(), "list")
	defer span.End()

	ids, err := s.engine.ListSnapshots(ctx)
	if err != nil {
		span.RecordError(err)
		s.logger.Error("list failed", "err", err)
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	span.SetAttributes(attribute.Int("snapshots.count", len(ids)))
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": ids})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("modelvault/api").Start(r.Context(), "restore")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("snapshot.id", id))
	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := s.engine.Restore(ctx, id, w); err != nil {
		span.RecordError(err)
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