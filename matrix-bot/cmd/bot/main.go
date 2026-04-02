// Package main is the entry point for the Matrix bot service.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/imdevinc/minions/matrix-bot/internal/clarify"
	"github.com/imdevinc/minions/matrix-bot/internal/handler"
	"github.com/imdevinc/minions/matrix-bot/internal/orchestrator"
)

// Version is set at build time via ldflags, defaults to dev for local builds.
var Version = "dev"

func main() {
	// Configure structured logging with JSON output for production
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("starting matrix-bot", "version", Version)

	// Get port from env or default to 8081
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	// MATRIX_HOMESERVER_URL is required (e.g., https://matrix.org)
	homeserverURL := os.Getenv("MATRIX_HOMESERVER_URL")
	if homeserverURL == "" {
		logger.Error("MATRIX_HOMESERVER_URL environment variable is required")
		os.Exit(1)
	}

	// MATRIX_BOT_USER_ID is required (e.g., @minion:matrix.org)
	botUserID := os.Getenv("MATRIX_BOT_USER_ID")
	if botUserID == "" {
		logger.Error("MATRIX_BOT_USER_ID environment variable is required")
		os.Exit(1)
	}

	// MATRIX_BOT_ACCESS_TOKEN is required
	accessToken := os.Getenv("MATRIX_BOT_ACCESS_TOKEN")
	if accessToken == "" {
		logger.Error("MATRIX_BOT_ACCESS_TOKEN environment variable is required")
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

	// OPENROUTER_API_KEY is required for clarification LLM
	openrouterKey := os.Getenv("OPENROUTER_API_KEY")
	if openrouterKey == "" {
		logger.Error("OPENROUTER_API_KEY environment variable is required")
		os.Exit(1)
	}

	// OPENROUTER_CLARIFICATION_MODEL is required for clarification LLM model selection
	clarificationModel := os.Getenv("OPENROUTER_CLARIFICATION_MODEL")
	if clarificationModel == "" {
		logger.Error("OPENROUTER_CLARIFICATION_MODEL environment variable is required")
		os.Exit(1)
	}

	// MATRIX_ALLOWED_ROOMS is optional, comma-separated list of allowed room IDs
	var allowedRooms []id.RoomID
	if allowedRoomsStr := os.Getenv("MATRIX_ALLOWED_ROOMS"); allowedRoomsStr != "" {
		for _, r := range strings.Split(allowedRoomsStr, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				allowedRooms = append(allowedRooms, id.RoomID(r))
			}
		}
		logger.Info("matrix room restriction enabled", "allowed_rooms", allowedRooms)
	}

	// MATRIX_ALLOWED_USERS is optional, comma-separated list of allowed user IDs
	var allowedUsers []id.UserID
	if allowedUsersStr := os.Getenv("MATRIX_ALLOWED_USERS"); allowedUsersStr != "" {
		for _, u := range strings.Split(allowedUsersStr, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				allowedUsers = append(allowedUsers, id.UserID(u))
			}
		}
		logger.Info("matrix user restriction enabled", "allowed_users", allowedUsers)
	}

	// Create orchestrator client for minion creation + rate limiting
	orchClient := orchestrator.NewClient(orchestratorURL, apiToken)

	// Create clarification LLM client and handler
	llmClient := clarify.NewOpenRouterClient(openrouterKey, clarificationModel)
	clarifyHandler := clarify.NewHandler(llmClient, logger)

	// Create Matrix client
	client, err := mautrix.NewClient(homeserverURL, id.UserID(botUserID), accessToken)
	if err != nil {
		logger.Error("failed to create Matrix client", "error", err)
		os.Exit(1)
	}

	// Create message handler
	msgHandler := handler.NewMessageHandler(
		logger,
		client,
		orchClient,
		clarifyHandler,
		id.UserID(botUserID),
		allowedRooms,
		allowedUsers,
	)

	// Set up syncer for receiving events
	syncer := client.Syncer.(*mautrix.DefaultSyncer)

	// Auto-accept room invites (respecting allowed rooms if configured)
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		// Only handle invites directed at the bot
		if evt.GetStateKey() != botUserID {
			return
		}
		membership := evt.Content.AsMember().Membership
		if membership != event.MembershipInvite {
			return
		}

		// If allowed rooms is configured, only accept invites to those rooms
		if len(allowedRooms) > 0 {
			allowed := false
			for _, r := range allowedRooms {
				if r == evt.RoomID {
					allowed = true
					break
				}
			}
			if !allowed {
				logger.Info("ignoring invite to non-allowed room", "room_id", evt.RoomID, "inviter", evt.Sender)
				return
			}
		}

		logger.Info("received room invite", "room_id", evt.RoomID, "inviter", evt.Sender)
		_, err := client.JoinRoomByID(ctx, evt.RoomID)
		if err != nil {
			logger.Error("failed to join room", "room_id", evt.RoomID, "error", err)
		} else {
			logger.Info("joined room", "room_id", evt.RoomID)
		}
	})

	// Handle message events
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		// First check if this is a reply to a clarification question
		msgHandler.HandleReply(ctx, evt)
		// Then handle as a potential mention/command
		msgHandler.Handle(ctx, evt)
	})

	// Log when we connect
	syncer.OnSync(func(ctx context.Context, resp *mautrix.RespSync, since string) bool {
		if since == "" {
			logger.Info("matrix client initial sync completed")
		}
		return true
	})

	// Start Matrix sync in a goroutine
	syncCtx, syncCancel := context.WithCancel(context.Background())
	go func() {
		logger.Info("starting Matrix sync")
		for {
			err := client.SyncWithContext(syncCtx)
			if err == nil || syncCtx.Err() != nil {
				break
			}
			logger.Error("matrix sync error, retrying in 5 seconds", "error", err)
			select {
			case <-syncCtx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()

	logger.Info("connected to Matrix homeserver", "user_id", botUserID, "homeserver", homeserverURL)

	// Set up HTTP server for webhook callbacks from orchestrator
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)

	// Health check endpoint (no auth required)
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Webhook handler for orchestrator callbacks
	webhookHandler := handler.NewWebhookHandler(logger, client, apiToken)
	router.Post("/webhook", webhookHandler.Handle)

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

	// Stop Matrix sync
	syncCancel()

	// Shutdown HTTP server gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server forced to shutdown", "error", err)
	}

	logger.Info("bot stopped")
}
