// Package main is the entry point for the minions orchestrator service.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/anomalyco/minions/orchestrator/internal/api"
	"github.com/anomalyco/minions/orchestrator/internal/db"
	"github.com/anomalyco/minions/orchestrator/internal/github"
	"github.com/anomalyco/minions/orchestrator/internal/k8s"
	"github.com/anomalyco/minions/orchestrator/internal/reconciler"
	"github.com/anomalyco/minions/orchestrator/internal/spawner"
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

	// DEVBOX_IMAGE is required for spawning minion pods
	devboxImage := os.Getenv("DEVBOX_IMAGE")
	if devboxImage == "" {
		logger.Error("DEVBOX_IMAGE environment variable is required")
		os.Exit(1)
	}

	// OPENROUTER_API_KEY is required for LLM access in devbox pods
	openRouterAPIKey := os.Getenv("OPENROUTER_API_KEY")
	if openRouterAPIKey == "" {
		logger.Error("OPENROUTER_API_KEY environment variable is required")
		os.Exit(1)
	}

	// GITHUB_APP_ID is required for GitHub App authentication
	githubAppIDStr := os.Getenv("GITHUB_APP_ID")
	if githubAppIDStr == "" {
		logger.Error("GITHUB_APP_ID environment variable is required")
		os.Exit(1)
	}
	githubAppID, err := strconv.ParseInt(githubAppIDStr, 10, 64)
	if err != nil {
		logger.Error("GITHUB_APP_ID must be a valid integer", "value", githubAppIDStr, "error", err)
		os.Exit(1)
	}

	// GITHUB_APP_PRIVATE_KEY is required for GitHub App authentication
	githubAppPrivateKey := os.Getenv("GITHUB_APP_PRIVATE_KEY")
	if githubAppPrivateKey == "" {
		logger.Error("GITHUB_APP_PRIVATE_KEY environment variable is required")
		os.Exit(1)
	}

	// ORCHESTRATOR_URL is required for pod callback URLs
	// (e.g., http://orchestrator.minions.svc.cluster.local:8080 in k8s)
	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		logger.Error("ORCHESTRATOR_URL environment variable is required")
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

	// Initialize Kubernetes client for pod management
	podManager, err := k8s.NewClient(k8s.Config{
		DevboxImage:      devboxImage,
		OpenRouterAPIKey: openRouterAPIKey,
	}, logger)
	if err != nil {
		logger.Error("failed to create kubernetes client", "error", err)
		os.Exit(1)
	}
	logger.Info("kubernetes client initialized", "devbox_image", devboxImage)

	// Initialize GitHub token manager for generating installation tokens
	tokenManager, err := github.NewManager(github.Config{
		AppID:      githubAppID,
		PrivateKey: []byte(githubAppPrivateKey),
	}, logger)
	if err != nil {
		logger.Error("failed to create GitHub token manager", "error", err)
		os.Exit(1)
	}
	logger.Info("github token manager initialized", "app_id", githubAppID)

	// Create webhook notifier for Discord bot callbacks
	// DISCORD_BOT_WEBHOOK_URL is optional; if not set, use no-op notifier
	var notifier webhook.Notifier
	webhookURL := os.Getenv("DISCORD_BOT_WEBHOOK_URL")
	if webhookURL != "" {
		notifier = webhook.NewHTTPNotifier(logger, webhookURL, apiToken)
		logger.Info("webhook notifier configured", "url", webhookURL)
	} else {
		notifier = webhook.NewNoOpNotifier(logger)
		logger.Info("webhook notifier not configured (DISCORD_BOT_WEBHOOK_URL not set)")
	}

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

	// Create SSE client for streaming events from pods
	// EventStore persists events, DBEventHandler routes to DB + WebSocket clients
	eventStore := db.NewEventStore(pool)
	eventHandler := streaming.NewDBEventHandler(streaming.DBEventHandlerConfig{
		EventStore:  eventStore,
		MinionStore: minionStore,
		Hub:         hub,
		Logger:      logger,
	})
	sseClient := streaming.NewSSEClient(podManager, eventHandler, streaming.SSEClientConfig{
		PodPort: 4096,
		Logger:  logger,
	})
	logger.Info("SSE client initialized", "pod_port", 4096)

	// Reconnect to active minions on orchestrator restart
	// Query minions that are running or pending (may have been spawned before restart)
	activeMinions, err := minionStore.ListByStatuses(ctx, []db.MinionStatus{db.StatusRunning, db.StatusPending})
	if err != nil {
		logger.Error("failed to query active minions for reconnection", "error", err)
		// Non-fatal - continue startup but log warning
	} else {
		reconnectCount := 0
		for _, m := range activeMinions {
			// Only reconnect if we have both a password and a pod name
			if m.OpencodePassword != nil && *m.OpencodePassword != "" && m.PodName != nil && *m.PodName != "" {
				logger.Info("reconnecting to minion", "minion_id", m.ID, "pod_name", *m.PodName)
				// Connect is fire-and-forget; it spawns a goroutine and handles retries internally
				sseClient.Connect(ctx, m.ID, *m.PodName, *m.OpencodePassword)
				reconnectCount++
			}
		}
		logger.Info("reconnection completed", "total_active", len(activeMinions), "reconnected", reconnectCount)
	}

	// Initialize spawner for background pod creation
	spwn := spawner.New(
		minionStore, // MinionQuerier
		minionStore, // MinionUpdater
		tokenManager,
		podManager,
		sseClient,
		spawner.Config{
			OrchestratorURL:  orchestratorURL,
			InternalAPIToken: apiToken,
		},
		logger,
	)

	// Start spawner (polls for pending minions)
	spawnerCtx, spawnerCancel := context.WithCancel(ctx)
	defer spawnerCancel()
	spwn.Start(spawnerCtx)
	logger.Info("spawner started")

	// Start the watchdog for idle minion detection and failed pod monitoring
	wdog := watchdog.New(minionStore, podManager, sseClient, notifier, logger)
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
		SSE:           sseClient,
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

	// Stop the spawner
	spwn.Stop()
	logger.Info("spawner stopped")

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
