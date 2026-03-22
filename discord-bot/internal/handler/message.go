// Package handler provides Discord message handlers for the minion bot.
package handler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/bwmarrin/discordgo"

	"github.com/anomalyco/minions/discord-bot/internal/command"
	"github.com/anomalyco/minions/discord-bot/internal/orchestrator"
)

// ThinkingEmoji is the reaction added when processing a command
const ThinkingEmoji = "🤔"

// MinionCreator creates minions via the orchestrator API.
// Abstraction allows for easy testing.
type MinionCreator interface {
	CreateMinion(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error)
}

// MessageHandler handles incoming Discord messages
type MessageHandler struct {
	logger       *slog.Logger
	orchestrator MinionCreator
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(logger *slog.Logger, orch MinionCreator) *MessageHandler {
	return &MessageHandler{
		logger:       logger,
		orchestrator: orch,
	}
}

// Handle processes a Discord message create event
func (h *MessageHandler) Handle(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from bots (including ourselves)
	if m.Author.Bot {
		return
	}

	// Check if we're mentioned
	if !command.IsMentioned(m.Content, s.State.User.ID) {
		return
	}

	h.logger.Info("received mention",
		"author", m.Author.Username,
		"author_id", m.Author.ID,
		"channel_id", m.ChannelID,
		"message_id", m.ID,
	)

	// Immediately react with thinking emoji to acknowledge
	if err := s.MessageReactionAdd(m.ChannelID, m.ID, ThinkingEmoji); err != nil {
		h.logger.Error("failed to add thinking reaction",
			"error", err,
			"channel_id", m.ChannelID,
			"message_id", m.ID,
		)
		// Continue processing even if reaction fails
	}

	// Strip the mention and parse the command
	text := command.StripMention(m.Content, s.State.User.ID)
	cmd, err := command.Parse(text)
	if err != nil {
		h.handleParseError(s, m, err)
		return
	}

	h.logger.Info("parsed command",
		"repo", cmd.Repo,
		"model", cmd.Model,
		"task_length", len(cmd.Task),
		"author_id", m.Author.ID,
		"message_id", m.ID,
	)

	// Create minion via orchestrator (enforces rate limits)
	resp, err := h.orchestrator.CreateMinion(context.Background(), orchestrator.CreateMinionRequest{
		Repo:             cmd.Repo,
		Task:             cmd.Task,
		Model:            cmd.Model,
		DiscordMessageID: m.ID,
		DiscordChannelID: m.ChannelID,
		DiscordUserID:    m.Author.ID,
		DiscordUsername:  m.Author.Username,
	})
	if err != nil {
		h.handleOrchestratorError(s, m, err)
		return
	}

	if resp.Duplicate {
		h.logger.Info("duplicate minion detected",
			"minion_id", resp.ID,
			"repo", cmd.Repo,
		)
		// Reply with link to existing minion
		msg := "⚠️ A minion is already working on this task. Check the existing one!"
		_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		if sendErr != nil {
			h.logger.Error("failed to send duplicate reply", "error", sendErr)
		}
		return
	}

	h.logger.Info("minion created",
		"minion_id", resp.ID,
		"repo", cmd.Repo,
		"model", cmd.Model,
	)

	// TODO: Send to clarification LLM or proceed to spawn pod directly
}

// handleParseError sends an error message to Discord for parse failures
func (h *MessageHandler) handleParseError(s *discordgo.Session, m *discordgo.MessageCreate, err error) {
	h.logger.Warn("command parse failed",
		"error", err,
		"author_id", m.Author.ID,
		"message_id", m.ID,
	)

	// Build a user-friendly error message
	var msg string
	switch {
	case isErrorType(err, command.ErrMissingRepo):
		msg = "❌ Missing `--repo` flag. Usage: `@minion --repo Owner/Repo <task>`"
	case isErrorType(err, command.ErrInvalidRepoFormat):
		msg = "❌ Invalid repo format. Expected `Owner/Repo` (e.g., `octocat/hello-world`)"
	case isErrorType(err, command.ErrMissingTask):
		msg = "❌ Missing task description. What should I do?"
	case isErrorType(err, command.ErrTaskTooLong):
		msg = "❌ Task is too long (max 10,000 characters)"
	case isErrorType(err, command.ErrTaskHasControl):
		msg = "❌ Task contains invalid characters"
	case isErrorType(err, command.ErrUnknownModel):
		msg = "❌ Unknown model. Allowed: `anthropic/*` or `openai/*`"
	default:
		msg = "❌ Failed to parse command: " + err.Error()
	}

	// Reply to the message
	_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
	if sendErr != nil {
		h.logger.Error("failed to send error reply",
			"error", sendErr,
			"original_error", err,
			"channel_id", m.ChannelID,
		)
	}
}

// handleOrchestratorError sends an error message to Discord for orchestrator failures
func (h *MessageHandler) handleOrchestratorError(s *discordgo.Session, m *discordgo.MessageCreate, err error) {
	h.logger.Warn("orchestrator request failed",
		"error", err,
		"author_id", m.Author.ID,
		"message_id", m.ID,
	)

	// Build a user-friendly error message
	var msg string
	switch {
	case errors.Is(err, orchestrator.ErrRateLimitExceeded):
		msg = "⏳ You've hit the hourly limit (10 minions/hour). Please wait a bit before spawning more."
	case errors.Is(err, orchestrator.ErrConcurrentLimitExceeded):
		msg = "⏳ You have too many minions running (max 3). Wait for some to finish!"
	default:
		msg = "❌ Failed to create minion: " + err.Error()
	}

	// Reply to the message
	_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
	if sendErr != nil {
		h.logger.Error("failed to send error reply",
			"error", sendErr,
			"original_error", err,
			"channel_id", m.ChannelID,
		)
	}
}

// isErrorType checks if err matches or wraps the target error
func isErrorType(err, target error) bool {
	// Check exact match or if err.Error() contains target.Error()
	// Using string comparison since errors.Is doesn't work for wrapped errors with fmt.Errorf
	if err == target {
		return true
	}
	if err != nil && target != nil {
		return containsError(err.Error(), target.Error())
	}
	return false
}

// containsError checks if errMsg contains targetMsg
func containsError(errMsg, targetMsg string) bool {
	return len(errMsg) >= len(targetMsg) && (errMsg == targetMsg ||
		(len(errMsg) > len(targetMsg) && errMsg[:len(targetMsg)] == targetMsg) ||
		containsSubstring(errMsg, targetMsg))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
