// Package main is the entry point for the github-webhook service.
// This service receives GitHub webhook events for PR comments/reviews
// and spawns follow-up minions when the bot is @mentioned.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/imdevinc/minions/github-webhook/internal/config"
	"github.com/imdevinc/minions/github-webhook/internal/github"
	"github.com/imdevinc/minions/github-webhook/internal/handler"
	"github.com/imdevinc/minions/github-webhook/internal/orchestrator"
)

func main() {
	// Configure structured logging with JSON output for production
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Initialize GitHub client (for reactions, comments, app info)
	ghClient, err := github.NewClient(github.Config{
		AppID:      cfg.GitHubAppID,
		PrivateKey: cfg.GitHubAppPrivateKey,
	}, logger)
	if err != nil {
		logger.Error("failed to create GitHub client", "error", err)
		os.Exit(1)
	}

	// Fetch bot username from GitHub App info
	ctx := context.Background()
	botUsername, err := ghClient.GetBotUsername(ctx)
	if err != nil {
		logger.Error("failed to get bot username", "error", err)
		os.Exit(1)
	}
	logger.Info("bot username resolved", "username", botUsername)

	// Initialize orchestrator client
	orchClient := orchestrator.NewClient(orchestrator.Config{
		BaseURL:  cfg.OrchestratorURL,
		APIToken: cfg.InternalAPIToken,
	}, logger)

	// Create webhook handler
	webhookHandler := handler.NewWebhookHandler(handler.Config{
		WebhookSecret: cfg.GitHubWebhookSecret,
		BotUsername:   botUsername,
		ApprovedRepos: cfg.ApprovedRepos,
		GitHubClient:  ghClient,
		Orchestrator:  orchClient,
		Logger:        logger,
	})

	// Setup router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// GitHub webhook endpoint
	r.Post("/webhook/github", webhookHandler.HandleWebhook)

	// Start server
	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("starting github-webhook server", "port", port)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}

	logger.Info("server stopped")
}
