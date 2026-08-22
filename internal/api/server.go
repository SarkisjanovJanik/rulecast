package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jsarkisjanov/rulecast/internal/eval"
	"github.com/jsarkisjanov/rulecast/internal/flag"
	"github.com/jsarkisjanov/rulecast/internal/store"
)

type Config struct {
	Environment string
	SDKKey      string
	AdminKey    string
	Heartbeat   time.Duration
	WebRoot     string
}

type Server struct {
	config    Config
	registry  *eval.Registry
	versions  store.Versions
	exposures store.Exposures
	reports   store.Reports
	metrics   *Metrics
	logger    *slog.Logger
	ready     func() bool
}

func NewServer(
	config Config,
	registry *eval.Registry,
	versions store.Versions,
	exposures store.Exposures,
	reports store.Reports,
	metrics *Metrics,
	logger *slog.Logger,
) *Server {
	if config.Heartbeat <= 0 {
		config.Heartbeat = 25 * time.Second
	}

	server := &Server{
		config:    config,
		registry:  registry,
		versions:  versions,
		exposures: exposures,
		reports:   reports,
		metrics:   metrics,
		logger:    logger,
	}
	server.ready = func() bool { return registry.Snapshot() != nil }
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	sdk := chain(s.sdkRoutes(), requireKey(s.logger, s.config.SDKKey))
	mux.Handle("/api/v1/sdk/", sdk)

	admin := chain(s.adminRoutes(), requireKey(s.logger, s.config.AdminKey))
	mux.Handle("/api/v1/admin/", admin)

	if s.config.WebRoot != "" {
		mux.Handle("/", http.FileServer(http.Dir(s.config.WebRoot)))
	}

	return chain(mux, withRequestID, withRecovery(s.logger), withLogging(s.logger))
}

func (s *Server) sdkRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/sdk/evaluate", s.handleEvaluate)
	mux.HandleFunc("POST /api/v1/sdk/evaluate-all", s.handleEvaluateAll)
	mux.HandleFunc("GET /api/v1/sdk/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /api/v1/sdk/stream", s.handleStream)
	return mux
}

func (s *Server) adminRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/schema", s.handleSchema)
	mux.HandleFunc("GET /api/v1/admin/flags", s.handleListFlags)
	mux.HandleFunc("GET /api/v1/admin/flags/{key}", s.handleGetFlag)
	mux.HandleFunc("PUT /api/v1/admin/flags/{key}", s.handlePutFlag)
	mux.HandleFunc("DELETE /api/v1/admin/flags/{key}", s.handleDeleteFlag)
	mux.HandleFunc("POST /api/v1/admin/validate", s.handleValidate)
	mux.HandleFunc("GET /api/v1/admin/versions", s.handleVersions)
	mux.HandleFunc("GET /api/v1/admin/versions/{version}", s.handleGetVersion)
	mux.HandleFunc("POST /api/v1/admin/versions/{version}/rollback", s.handleRollback)
	mux.HandleFunc("GET /api/v1/admin/experiments/{key}", s.handleExperiment)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !s.ready() {
		writeProblem(w, s.logger, http.StatusServiceUnavailable, "NOT_READY",
			"no flag set version has been loaded yet")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	var version int64
	var flags int
	if snapshot := s.registry.Snapshot(); snapshot != nil {
		version = snapshot.Version()
		flags = len(snapshot.Keys())
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.metrics.WriteTo(w, version, flags)
}

func (s *Server) publish(ctx context.Context, document flag.Document) (*eval.Snapshot, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}

	snapshot, err := eval.Compile(document)
	if err != nil {
		return nil, err
	}
	if err := s.versions.Append(ctx, document); err != nil {
		return nil, err
	}

	s.registry.Publish(snapshot)
	s.metrics.Publish()
	s.logger.Info("published version",
		"environment", document.Environment,
		"version", document.Version,
		"flags", len(document.Flags),
		"by", document.CreatedBy)

	return snapshot, nil
}
