package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jsarkisjanov/rulecast/internal/api"
	"github.com/jsarkisjanov/rulecast/internal/config"
	"github.com/jsarkisjanov/rulecast/internal/eval"
	flagset "github.com/jsarkisjanov/rulecast/internal/flag"
	"github.com/jsarkisjanov/rulecast/internal/store"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false,
		"probe the local readiness endpoint and exit; used by the container healthcheck")
	flag.Parse()

	if *healthcheck {
		if err := probe(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: settings.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	registry := eval.NewRegistry()
	metrics := api.NewMetrics()

	versions, exposures, reports, writer, closeStore, err := openStore(ctx, settings, logger, metrics)
	if err != nil {
		return err
	}
	defer closeStore()

	if err := activate(ctx, settings, versions, registry, logger); err != nil {
		return err
	}

	server := api.NewServer(api.Config{
		Environment: settings.Environment,
		SDKKey:      settings.SDKKey,
		AdminKey:    settings.AdminKey,
		Heartbeat:   settings.Heartbeat,
		WebRoot:     settings.WebRoot,
	}, registry, versions, exposures, reports, metrics, logger)

	httpServer := &http.Server{
		Addr:              settings.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       settings.ReadTimeout,
		WriteTimeout:      settings.WriteTimeout,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("listening",
			"addr", settings.Addr,
			"environment", settings.Environment,
			"storage", storageName(settings))
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	case <-ctx.Done():
		logger.Info("shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown timed out, closing", "error", err)
		_ = httpServer.Close()
	}

	if writer != nil {
		writer.Wait()
	}

	logger.Info("stopped")
	return nil
}

func openStore(
	ctx context.Context,
	settings config.Config,
	logger *slog.Logger,
	metrics *api.Metrics,
) (store.Versions, store.Exposures, store.Reports, *store.ExposureWriter, func(), error) {
	if settings.InMemory() {
		memory := store.NewMemory()
		logger.Warn("running without a database; published versions will not survive a restart")
		return memory, memory, memory, nil, func() {}, nil
	}

	pool, err := pgxpool.New(ctx, settings.DatabaseURL)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("open database pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("reach database: %w", err)
	}

	if err := store.Migrate(ctx, pool, logger); err != nil {
		pool.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("migrate: %w", err)
	}

	writer := store.NewExposureWriter(pool, logger, store.ExposureWriterConfig{})
	metrics.ExposuresDropped = writer.Dropped
	go writer.Run(ctx)

	postgres := store.NewPostgres(pool)
	return postgres, writer, postgres, writer, pool.Close, nil
}

func activate(
	ctx context.Context,
	settings config.Config,
	versions store.Versions,
	registry *eval.Registry,
	logger *slog.Logger,
) error {
	document, err := versions.Latest(ctx, settings.Environment)
	switch {
	case err == nil:
	case errors.Is(err, flagset.ErrNotFound):
		document = flagset.Seed(settings.Environment, time.Now())
		if err := versions.Append(ctx, document); err != nil {
			return fmt.Errorf("seed environment: %w", err)
		}
		logger.Info("seeded environment", "environment", settings.Environment, "flags", len(document.Flags))
	default:
		return fmt.Errorf("load latest version: %w", err)
	}

	snapshot, err := eval.Compile(document)
	if err != nil {
		return fmt.Errorf("compile version %d: %w", document.Version, err)
	}

	registry.Publish(snapshot)
	logger.Info("activated version",
		"version", snapshot.Version(),
		"flags", len(snapshot.Keys()))
	return nil
}

func storageName(settings config.Config) string {
	if settings.InMemory() {
		return "memory"
	}
	return "postgres"
}

func probe() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}

	_, port, err := net.SplitHostPort(settings.Addr)
	if err != nil {
		return fmt.Errorf("parse %q: %w", settings.Addr, err)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	url := "http://127.0.0.1:" + strings.TrimSpace(port) + "/readyz"

	response, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("probe %s: %w", url, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("probe %s: status %d", url, response.StatusCode)
	}
	return nil
}
