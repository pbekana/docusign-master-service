// Package main is the entry point for the DocuSign Master Service.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/your-org/docusign-master-service/internal/audit"
	"github.com/your-org/docusign-master-service/internal/config"
	"github.com/your-org/docusign-master-service/internal/database"
	"github.com/your-org/docusign-master-service/internal/docusign"
	"github.com/your-org/docusign-master-service/internal/httpapi"
	"github.com/your-org/docusign-master-service/internal/httpapi/handlers"
	"github.com/your-org/docusign-master-service/internal/jobs"
	"github.com/your-org/docusign-master-service/internal/security"
	"github.com/your-org/docusign-master-service/internal/storage"
)

const version = "0.1.0"

func main() {
	// --- Logging ---
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// --- Configuration ---
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	// Adjust log level from config.
	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Database ---
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := database.Ping(ctx, pool); err != nil {
		slog.Error("database ping failed", "error", err)
		os.Exit(1)
	}

	// Resolve migrations directory relative to the binary's working directory.
	migrationsDir := filepath.Join(findProjectRoot(), "migrations")
	if err := database.RunMigrations(ctx, pool, migrationsDir); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	// --- Services ---
	auditRepo := audit.NewAuditRepository(pool)
	auditSvc := audit.NewAuditService(auditRepo, logger)

	dsAuth, err := docusign.NewJWTAuthClient(cfg)
	if err != nil {
		slog.Error("failed to create DocuSign auth client", "error", err)
		os.Exit(1)
	}

	dsClient := docusign.NewDSClient(dsAuth, cfg)

	localStore := storage.NewLocalStorage(cfg.LocalOutputDir)

	var minioStore *storage.MinIOStorage
	if cfg.MinioEndpoint != "" {
		ms, err := storage.NewMinIOStorage(cfg)
		if err != nil {
			slog.Warn("MinIO storage unavailable", "error", err)
		} else {
			minioStore = ms
		}
	}

	jobRepo := jobs.NewJobRepository(pool)
	cpRepo := jobs.NewCheckpointRepository(pool)

	jobSvc := jobs.NewJobService(
		jobRepo, cpRepo,
		dsClient,
		localStore, minioStore,
		auditSvc,
		logger,
		cfg,
	)

	workerPool := jobs.NewWorkerPool(jobSvc, jobRepo, logger, cfg)
	workerPool.Start(ctx)

	// Recover stale/pending jobs from before restart.
	workerPool.RecoverOnStartup(ctx)

	// --- Nonce store ---
	nonces := security.NewNonceStore(ctx)

	// --- HTTP handlers ---
	healthH := handlers.NewHealthHandler(version, pool, jobRepo)
	credH := handlers.NewCredentialsHandler(dsAuth)
	scanH := handlers.NewScanHandler(jobSvc, jobRepo, workerPool, auditSvc)
	normH := handlers.NewNormalizationHandler(jobSvc, jobRepo, cfg.LocalOutputDir)
	maintH := handlers.NewMaintenanceHandler(jobRepo, workerPool, cfg.CrashTimeoutMinutes)
	auditH := handlers.NewAuditHandler(auditSvc, auditRepo)
	swaggerH := handlers.NewSwaggerHandler()

	router := httpapi.NewRouter(cfg, nonces, auditSvc, healthH, credH, scanH, normH, maintH, auditH, swaggerH)

	// --- HTTP server ---
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server.
	go func() {
		slog.Info("DocuSign Master Service listening", "addr", srv.Addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal.
	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Drain worker pool first.
	if err := workerPool.Shutdown(shutdownCtx); err != nil {
		slog.Warn("worker pool shutdown incomplete", "error", err)
	}

	// Then shut down HTTP server.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP server shutdown incomplete", "error", err)
	}

	slog.Info("shutdown complete")
}

// findProjectRoot returns the directory from which to resolve migrations.
// In production it returns the working directory; in development it walks up
// to find the go.mod file.
func findProjectRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	// Walk up until we find go.mod.
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}
