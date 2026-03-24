// Package handler provides Discord message handlers for the minion bot.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/anomalyco/minions/discord-bot/internal/clarify"
	"github.com/anomalyco/minions/discord-bot/internal/command"
	"github.com/anomalyco/minions/discord-bot/internal/orchestrator"
)

// ThinkingEmoji is the reaction added when processing a command
const ThinkingEmoji = "🤔"

// OperationTimeout is the maximum duration for orchestrator API calls
const OperationTimeout = 30 * time.Second

// MinionCreator creates minions via the orchestrator API.
// Abstraction allows for easy testing.
type MinionCreator interface {
	CreateMinion(ctx context.Context, req orchestrator.CreateMinionRequest) (*orchestrator.CreateMinionResponse, error)
}

// MinionUpdater updates minion state via the orchestrator API.
type MinionUpdater interface {
	SetClarificationAnswer(ctx context.Context, minionID string, answer string) error
}

// ClarificationLookup looks up minions by clarification message ID.
type ClarificationLookup interface {
	GetByClarificationMessageID(ctx context.Context, messageID string) (*orchestrator.MinionByClarificationResponse, error)
}

// Orchestrator combines minion creation, update, and lookup capabilities.
type Orchestrator interface {
	MinionCreator
	MinionUpdater
	ClarificationLookup
}

// ClarificationEvaluator evaluates tasks for clarification needs.
type ClarificationEvaluator interface {
	EvaluateWithRetry(ctx context.Context, repo, task string) (*clarify.Result, error)
}

// MessageHandler handles incoming Discord messages
type MessageHandler struct {
	logger         *slog.Logger
	orchestrator   Orchestrator
	clarification  ClarificationEvaluator
	allowedGuildID string
	allowedRoleID  string
}

// AccessRestrictions configures optional Discord command access restrictions.
type AccessRestrictions struct {
	AllowedGuildID string
	AllowedRoleID  string
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(
	logger *slog.Logger,
	orch Orchestrator,
	clarification ClarificationEvaluator,
	restrictions AccessRestrictions,
) *MessageHandler {
	return &MessageHandler{
		logger:         logger,
		orchestrator:   orch,
		clarification:  clarification,
		allowedGuildID: restrictions.AllowedGuildID,
		allowedRoleID:  restrictions.AllowedRoleID,
	}
}

// Handle processes a Discord message create event
func (h *MessageHandler) Handle(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from bots (including ourselves)
	if m.Author.Bot {
		return
	}

	if !h.isCommandAllowed(s, m.Message) {
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
	} else {
		// Remove thinking emoji when processing completes (success or failure)
		defer func() {
			if err := s.MessageReactionRemove(m.ChannelID, m.ID, ThinkingEmoji, "@me"); err != nil {
				h.logger.Debug("failed to remove thinking reaction",
					"error", err,
					"channel_id", m.ChannelID,
					"message_id", m.ID,
				)
			}
		}()
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

	// Create timeout context for clarification evaluation
	evalCtx, evalCancel := context.WithTimeout(context.Background(), OperationTimeout)
	defer evalCancel()

	// Evaluate clarification first, then create a minion exactly once with the correct initial state.
	result, err := h.clarification.EvaluateWithRetry(evalCtx, cmd.Repo, cmd.Task)
	if err != nil {
		h.logger.Error("clarification LLM failed after retries",
			"error", err,
			"repo", cmd.Repo,
		)
		msg := "❌ Failed to evaluate task clarity. Please try again."
		_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		if sendErr != nil {
			h.logger.Error("failed to send failure notification", "error", sendErr)
		}
		return
	}

	if result.Ready {
		// Create timeout context for minion creation
		createCtx, createCancel := context.WithTimeout(context.Background(), OperationTimeout)
		defer createCancel()

		// Create minion in pending state (spawner will pick it up)
		resp, createErr := h.orchestrator.CreateMinion(createCtx, orchestrator.CreateMinionRequest{
			Repo:             cmd.Repo,
			Task:             cmd.Task,
			Model:            cmd.Model,
			InitialStatus:    "pending",
			DiscordMessageID: m.ID,
			DiscordChannelID: m.ChannelID,
			DiscordUserID:    m.Author.ID,
			DiscordUsername:  m.Author.Username,
		})
		if createErr != nil {
			h.handleOrchestratorError(s, m, createErr)
			return
		}

		if resp.Duplicate {
			h.logger.Info("duplicate minion detected",
				"minion_id", resp.ID,
				"repo", cmd.Repo,
			)
			msg := "⚠️ A minion is already working on this task. Check the existing one!"
			_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
			if sendErr != nil {
				h.logger.Error("failed to send duplicate reply", "error", sendErr)
			}
			return
		}

		h.logger.Info("task is ready, minion created",
			"minion_id", resp.ID,
			"repo", cmd.Repo,
		)
		msg := "✅ Task is clear! Your minion is being spawned..."
		_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		if sendErr != nil {
			h.logger.Error("failed to send ready notification", "error", sendErr)
		}
		return
	}

	// Task needs clarification - ask question first, then create minion in awaiting_clarification.
	clarificationMsg, sendErr := s.ChannelMessageSendReply(
		m.ChannelID,
		"❓ "+result.Question+"\n\n*Reply to this message with your answer.*",
		m.Reference(),
	)
	if sendErr != nil {
		h.logger.Error("failed to send clarification question",
			"error", sendErr,
			"repo", cmd.Repo,
		)
		msg := "❌ Failed to ask clarification question. Please try again."
		_, _ = s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		return
	}

	// Create timeout context for minion creation with clarification
	clarifyCtx, clarifyCancel := context.WithTimeout(context.Background(), OperationTimeout)
	defer clarifyCancel()

	resp, createErr := h.orchestrator.CreateMinion(clarifyCtx, orchestrator.CreateMinionRequest{
		Repo:                   cmd.Repo,
		Task:                   cmd.Task,
		Model:                  cmd.Model,
		InitialStatus:          "awaiting_clarification",
		ClarificationQuestion:  result.Question,
		ClarificationMessageID: clarificationMsg.ID,
		DiscordMessageID:       m.ID,
		DiscordChannelID:       m.ChannelID,
		DiscordUserID:          m.Author.ID,
		DiscordUsername:        m.Author.Username,
	})
	if createErr != nil {
		_ = s.ChannelMessageDelete(m.ChannelID, clarificationMsg.ID)
		h.handleOrchestratorError(s, m, createErr)
		return
	}

	if resp.Duplicate {
		// Clean up question we just posted; existing minion already handles this task.
		_ = s.ChannelMessageDelete(m.ChannelID, clarificationMsg.ID)
		h.logger.Info("duplicate minion detected during clarification",
			"minion_id", resp.ID,
			"repo", cmd.Repo,
		)
		msg := "⚠️ A minion is already working on this task. Check the existing one!"
		_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		if sendErr != nil {
			h.logger.Error("failed to send duplicate reply", "error", sendErr)
		}
		return
	}

	h.logger.Info("clarification question posted and minion created",
		"minion_id", resp.ID,
		"repo", cmd.Repo,
		"clarification_message_id", clarificationMsg.ID,
	)
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
	case errors.Is(err, command.ErrMissingRepo):
		msg = "❌ Missing `--repo` flag. Usage: `@minion --repo Owner/Repo <task>`"
	case errors.Is(err, command.ErrInvalidRepoFormat):
		msg = "❌ Invalid repo format. Expected `Owner/Repo` (e.g., `octocat/hello-world`)"
	case errors.Is(err, command.ErrMissingTask):
		msg = "❌ Missing task description. What should I do?"
	case errors.Is(err, command.ErrTaskTooLong):
		msg = "❌ Task is too long (max 10,000 characters)"
	case errors.Is(err, command.ErrTaskHasControl):
		msg = "❌ Task contains invalid characters"
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

// HandleReply processes a Discord message that is a reply to another message.
// If the replied-to message is a clarification question, it processes the answer.
func (h *MessageHandler) HandleReply(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from bots
	if m.Author.Bot {
		return
	}

	// Check if this is a reply to another message
	if m.MessageReference == nil || m.MessageReference.MessageID == "" {
		return
	}

	if !h.isCommandAllowed(s, m.Message) {
		return
	}

	// Create timeout context for orchestrator calls
	ctx, cancel := context.WithTimeout(context.Background(), OperationTimeout)
	defer cancel()

	referencedMsgID := m.MessageReference.MessageID

	// Look up minion by the referenced message ID (could be a clarification question)
	minion, err := h.orchestrator.GetByClarificationMessageID(ctx, referencedMsgID)
	if err != nil {
		if errors.Is(err, orchestrator.ErrClarificationNotFound) {
			// Not a clarification message, ignore
			return
		}
		h.logger.Error("failed to look up clarification",
			"error", err,
			"referenced_message_id", referencedMsgID,
		)
		return
	}

	h.logger.Info("received clarification reply",
		"minion_id", minion.ID,
		"author", m.Author.Username,
		"author_id", m.Author.ID,
		"channel_id", m.ChannelID,
	)

	// Check if minion is still awaiting clarification
	if minion.Status != "awaiting_clarification" {
		h.logger.Warn("minion not awaiting clarification",
			"minion_id", minion.ID,
			"status", minion.Status,
		)
		msg := "⚠️ This minion is no longer waiting for clarification (status: " + minion.Status + ")"
		_, _ = s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		return
	}

	// Validate that the reply is from the original requester
	if minion.DiscordUserID != m.Author.ID {
		h.logger.Warn("clarification reply from wrong user",
			"minion_id", minion.ID,
			"expected_user", minion.DiscordUserID,
			"actual_user", m.Author.ID,
		)
		msg := "⚠️ Only the original requester can answer clarification questions."
		_, _ = s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		return
	}

	// React with thinking emoji to acknowledge
	if err := s.MessageReactionAdd(m.ChannelID, m.ID, ThinkingEmoji); err != nil {
		h.logger.Error("failed to add thinking reaction",
			"error", err,
			"channel_id", m.ChannelID,
			"message_id", m.ID,
		)
	} else {
		// Remove thinking emoji when processing completes (success or failure)
		defer func() {
			if err := s.MessageReactionRemove(m.ChannelID, m.ID, ThinkingEmoji, "@me"); err != nil {
				h.logger.Debug("failed to remove thinking reaction",
					"error", err,
					"channel_id", m.ChannelID,
					"message_id", m.ID,
				)
			}
		}()
	}

	// Get the answer (the content of the reply)
	answer := m.Content
	if answer == "" {
		msg := "❌ Your reply is empty. Please provide an answer to the clarification question."
		_, _ = s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		return
	}

	// Set the clarification answer and transition minion back to pending
	err = h.orchestrator.SetClarificationAnswer(ctx, minion.ID, answer)
	if err != nil {
		if errors.Is(err, orchestrator.ErrClarificationNotFound) {
			// Minion was deleted or clarification message changed (rare race)
			msg := "❌ The minion for this clarification no longer exists."
			_, _ = s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
			return
		}
		h.logger.Error("failed to set clarification answer",
			"error", err,
			"minion_id", minion.ID,
		)
		msg := "❌ Failed to process your answer. Please try again or contact support."
		_, _ = s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		return
	}

	h.logger.Info("clarification answer accepted",
		"minion_id", minion.ID,
		"answer_length", len(answer),
	)

	// Confirm to the user
	msg := "✅ Got it! Your minion is now being spawned with the clarified task..."
	_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
	if sendErr != nil {
		h.logger.Error("failed to send confirmation", "error", sendErr)
	}
}

// isCommandAllowed checks optional guild/role restrictions for command execution.
func (h *MessageHandler) isCommandAllowed(s *discordgo.Session, msg *discordgo.Message) bool {
	if h.allowedGuildID == "" && h.allowedRoleID == "" {
		return true
	}

	if msg.GuildID == "" {
		h.logger.Info("ignoring command from non-guild context",
			"author_id", msg.Author.ID,
			"channel_id", msg.ChannelID,
		)
		return false
	}

	if h.allowedGuildID != "" && msg.GuildID != h.allowedGuildID {
		h.logger.Info("ignoring command from unauthorized guild",
			"author_id", msg.Author.ID,
			"channel_id", msg.ChannelID,
			"guild_id", msg.GuildID,
		)
		return false
	}

	if h.allowedRoleID == "" {
		return true
	}

	roles, err := h.resolveMemberRoles(s, msg)
	if err != nil {
		h.logger.Warn("failed to resolve member roles for command authorization",
			"error", err,
			"author_id", msg.Author.ID,
			"guild_id", msg.GuildID,
		)
		return false
	}

	if !hasRole(roles, h.allowedRoleID) {
		h.logger.Info("ignoring command from user without required role",
			"author_id", msg.Author.ID,
			"guild_id", msg.GuildID,
		)
		return false
	}

	return true
}

func (h *MessageHandler) resolveMemberRoles(s *discordgo.Session, msg *discordgo.Message) ([]string, error) {
	if msg.Member != nil {
		return msg.Member.Roles, nil
	}

	member, err := s.GuildMember(msg.GuildID, msg.Author.ID)
	if err != nil {
		return nil, err
	}

	return member.Roles, nil
}

func hasRole(roleIDs []string, requiredRoleID string) bool {
	for _, roleID := range roleIDs {
		if roleID == requiredRoleID {
			return true
		}
	}

	return false
}
