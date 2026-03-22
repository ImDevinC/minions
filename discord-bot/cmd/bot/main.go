// Package main is the entry point for the Discord bot service.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/anomalyco/minions/discord-bot/internal/clarify"
	"github.com/anomalyco/minions/discord-bot/internal/handler"
	"github.com/anomalyco/minions/discord-bot/internal/orchestrator"
)

func main() {
	// Configure structured logging with JSON output for production
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Get port from env or default to 8081
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	// DISCORD_BOT_TOKEN is required
	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	if botToken == "" {
		logger.Error("DISCORD_BOT_TOKEN environment variable is required")
		os.Exit(1)
	}

	// ORCHESTRATOR_URL is required
	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		logger.Error("ORCHESTRATOR_URL environment variable is required")
		os.Exit(1)
	}

	// INTERNAL_API_TOKEN is required
	apiToken := os.Getenv("INTERNAL_API_TOKEN")
	if apiToken == "" {
		logger.Error("INTERNAL_API_TOKEN environment variable is required")
		os.Exit(1)
	}

	// ANTHROPIC_API_KEY is required for clarification LLM
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		logger.Error("ANTHROPIC_API_KEY environment variable is required")
		os.Exit(1)
	}

	// Create orchestrator client for minion creation + rate limiting
	orchClient := orchestrator.NewClient(orchestratorURL, apiToken)

	// Create clarification LLM client and handler
	llmClient := clarify.NewAnthropicClient(anthropicKey)
	clarifyHandler := clarify.NewHandler(llmClient, logger)

	// Create Discord session
	discord, err := discordgo.New("Bot " + botToken)
	if err != nil {
		logger.Error("failed to create Discord session", "error", err)
		os.Exit(1)
	}

	// Set intents for gateway - we need guild messages and message content
	discord.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuilds

	// Add ready handler to confirm connection
	discord.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		logger.Info("discord bot connected",
			"username", r.User.Username,
			"discriminator", r.User.Discriminator,
			"guilds", len(r.Guilds),
		)
	})

	// Add message handler for @minion mentions
	msgHandler := handler.NewMessageHandler(logger, orchClient, clarifyHandler)
	discord.AddHandler(msgHandler.Handle)
	// Also handle replies to clarification questions
	discord.AddHandler(msgHandler.HandleReply)

	// Open connection to Discord gateway
	if err := discord.Open(); err != nil {
		logger.Error("failed to open Discord connection", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := discord.Close(); err != nil {
			logger.Error("failed to close Discord connection", "error", err)
		}
	}()

	logger.Info("connected to Discord gateway")

	// Set up HTTP server for webhook callbacks from orchestrator
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	// Health check endpoint
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in goroutine
	go func() {
		logger.Info("starting HTTP server", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down bot")

	// Shutdown HTTP server gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server forced to shutdown", "error", err)
	}

	logger.Info("bot stopped")
}
