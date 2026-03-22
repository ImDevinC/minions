// Package main is the entry point for the minions orchestrator service.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anomalyco/minions/orchestrator/internal/api"
	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/anomalyco/minions/orchestrator/internal/k8s"
	"github.com/anomalyco/minions/orchestrator/internal/reconciler"
	"github.com/anomalyco/minions/orchestrator/internal/streaming"
	"github.com/anomalyco/minions/orchestrator/internal/watchdog"
	"github.com/anomalyco/minions/orchestrator/internal/webhook"
)

func main() {
	// Configure structured logging with JSON output for production
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Get port from env or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// INTERNAL_API_TOKEN is required for authenticating /api/* routes
	apiToken := os.Getenv("INTERNAL_API_TOKEN")
	if apiToken == "" {
		logger.Error("INTERNAL_API_TOKEN environment variable is required")
		os.Exit(1)
	}

	// DATABASE_URL is required for PostgreSQL connection
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	// Connect to database
	ctx := context.Background()
	pool, err := db.Connect(ctx, db.Config{
		DSN:             databaseURL,
		MaxConns:        25,
		MinConns:        5,
		MaxConnIdleTime: 5 * time.Minute,
	})
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	logger.Info("connected to database")

	// TODO(k8s-1): Replace with real k8s.NewClient once k8s is configured
	podManager := k8s.NewNoOpPodManager(logger)

	// TODO(bot-6): Replace with real webhook.NewNotifier once webhook client is implemented
	notifier := webhook.NewNoOpNotifier(logger)

	// Create stores
	minionStore := db.NewMinionStore(pool)

	// Run reconciliation BEFORE starting HTTP server
	// This ensures DB state is consistent with k8s state
	rec := reconciler.New(minionStore, podManager, logger)
	result, err := rec.Run(ctx)
	if err != nil {
		logger.Error("reconciliation failed", "error", err)
		os.Exit(1)
	}
	logger.Info("reconciliation completed",
		"orphaned_minions", result.OrphanedMinions,
		"stray_pods", result.StrayPods,
	)

	// Create and start WebSocket hub for live event streaming
	hub := streaming.NewHub(logger)
	hubCtx, hubCancel := context.WithCancel(ctx)
	defer hubCancel()
	go hub.Run(hubCtx)
	logger.Info("websocket hub started")

	// Start the watchdog for idle minion detection and failed pod monitoring
	wdog := watchdog.New(minionStore, podManager, notifier, logger)
	watchdogCtx, watchdogCancel := context.WithCancel(ctx)
	defer watchdogCancel()
	go wdog.Run(watchdogCtx)
	logger.Info("watchdog started")

	router := api.NewRouter(api.RouterConfig{
		Logger:        logger,
		APIToken:      apiToken,
		Pool:          pool,
		PodTerminator: podManager,
		Notifier:      notifier,
		Hub:           hub,
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("starting orchestrator", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")

	// Stop the watchdog
	wdog.Stop()
	logger.Info("watchdog stopped")

	// Stop the WebSocket hub
	hub.Stop()
	logger.Info("websocket hub stopped")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}

	logger.Info("server stopped")
}
