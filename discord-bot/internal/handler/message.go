// Package handler provides Discord message handlers for the minion bot.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/imdevinc/minions/discord-bot/internal/clarify"
	"github.com/imdevinc/minions/discord-bot/internal/command"
	"github.com/imdevinc/minions/discord-bot/internal/orchestrator"
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
	SetClarification(context.Context, string, orchestrator.SetClarificationRequest) error
	ApplyClarificationReply(context.Context, string, orchestrator.SetClarificationAnswerRequest) error
}

// ClarificationEvaluator evaluates tasks for clarification needs.
type ClarificationEvaluator interface {
	EvaluateWithRetry(ctx context.Context, repo, task string) (*clarify.Result, error)
}

// MessageHandler handles incoming Discord messages
type MessageHandler struct {
	logger          *slog.Logger
	orchestrator    Orchestrator
	clarification   ClarificationEvaluator
	allowedGuildID  string
	allowedRoleID   string
	controlPanelURL string
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
	controlPanelURL string,
) *MessageHandler {
	return &MessageHandler{
		logger:          logger,
		orchestrator:    orch,
		clarification:   clarification,
		allowedGuildID:  restrictions.AllowedGuildID,
		allowedRoleID:   restrictions.AllowedRoleID,
		controlPanelURL: controlPanelURL,
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
		msg := "❌ Failed to evaluate task clarity. Please try again.\n\nError: " + err.Error()
		_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		if sendErr != nil {
			h.logger.Error("failed to send failure notification", "error", sendErr)
		}
		return
	}

	h.submitWorkflow(s, m, cmd, result)
}

// submitWorkflow saves the first question before posting it to Discord.
func (h *MessageHandler) submitWorkflow(s *discordgo.Session, m *discordgo.MessageCreate, cmd *command.Command, result *clarify.Result) {
	ctx, cancel := context.WithTimeout(context.Background(), OperationTimeout)
	defer cancel()
	request := orchestrator.CreateMinionRequest{Repo: cmd.Repo, Task: cmd.Task, Model: cmd.Model,
		DiscordUserID: m.Author.ID, DiscordUsername: m.Author.Username, DiscordMessageID: m.ID, DiscordChannelID: m.ChannelID,
		InitialStatus: "pending"}
	if !result.Ready {
		request.InitialStatus, request.ClarificationQuestion = "awaiting_clarification", result.Question
	}
	run, err := h.orchestrator.CreateMinion(ctx, request)
	if err != nil {
		h.handleOrchestratorError(s, m, err)
		return
	}
	if run.Status == "awaiting_clarification" && run.Question != nil && run.QuestionID == nil {
		question, err := sendWorkflowQuestion(s, m, "❓ "+*run.Question+"\n\nReply to this message.")
		if err != nil {
			h.logger.Warn("could not deliver workflow question", "run", run.ID, "error", err)
			_, _ = s.ChannelMessageSendReply(m.ChannelID, "Could not send the question. Please submit the task again.", m.Reference())
			return
		}
		if err := h.orchestrator.SetClarification(ctx, run.ID, orchestrator.SetClarificationRequest{
			Question: *run.Question, DiscordMessageID: question.ID,
		}); err != nil {
			h.logger.Warn("could not bind workflow question", "run", run.ID, "error", err)
		}
		return
	}
	message := "Run " + run.ID + ": " + strings.ReplaceAll(run.Status, "_", " ") + "."
	if h.controlPanelURL != "" {
		message += "\n" + h.controlPanelURL + "/minions/" + run.ID
	}
	_, _ = s.ChannelMessageSendReply(m.ChannelID, message, m.Reference())
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
	if minion.WorkflowRound > 0 {
		h.handleWorkflowReply(ctx, s, m, minion, answer)
		return
	}
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
	if h.controlPanelURL != "" {
		msg += fmt.Sprintf("\nView progress: %s/minions/%s", h.controlPanelURL, minion.ID)
	}
	_, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
	if sendErr != nil {
		h.logger.Error("failed to send confirmation", "error", sendErr)
	}
}

// handleWorkflowReply rechecks the task before the runner can start a pod.
func (h *MessageHandler) handleWorkflowReply(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, minion *orchestrator.MinionByClarificationResponse, answer string) {
	if minion.DiscordChannelID == nil || *minion.DiscordChannelID != m.ChannelID {
		return
	}
	task := minion.Task
	if minion.ClarificationAnswer != nil {
		task += "\n\nPrevious requester clarification:\n" + *minion.ClarificationAnswer
	}
	if minion.ClarificationQuestion != nil {
		task += "\n\nQuestion asked:\n" + *minion.ClarificationQuestion
	}
	task += "\n\nRequester reply:\n" + answer
	result, err := h.clarification.EvaluateWithRetry(ctx, minion.Repo, task)
	if err != nil {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "Could not check the answer. Reply again to retry.", m.Reference())
		return
	}
	req := orchestrator.SetClarificationAnswerRequest{
		Answer: answer, Author: m.Author.ID, Channel: m.ChannelID, Expected: m.MessageReference.MessageID,
	}
	if !result.Ready {
		if minion.WorkflowRound >= 2 {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, "The task is still unclear. Please submit a new request with the missing details.", m.Reference())
			return
		}
		question, err := sendWorkflowQuestion(s, m, "One follow-up:\n"+result.Question+"\n\nReply to this message.")
		if err != nil {
			return
		}
		req.Question, req.Message = result.Question, question.ID
	}
	if err := h.orchestrator.ApplyClarificationReply(ctx, minion.ID, req); err != nil {
		if req.Message != "" {
			_ = s.ChannelMessageDelete(m.ChannelID, req.Message)
		}
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "Could not save this reply. Check that it answers the current question.", m.Reference())
		return
	}
	if result.Ready {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "The clarified task is queued.", m.Reference())
	}
}

// sendWorkflowQuestion keeps model output from mentioning Discord users or roles.
func sendWorkflowQuestion(s *discordgo.Session, message *discordgo.MessageCreate, text string) (*discordgo.Message, error) {
	return s.ChannelMessageSendComplex(message.ChannelID, &discordgo.MessageSend{
		Content: text, Reference: message.Reference(), AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	})
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
		// Send "I'm sorry Dave" message for non-guild context
		if s != nil {
			_, sendErr := s.ChannelMessageSendReply(
				msg.ChannelID,
				"I'm sorry Dave, I'm afraid I can't do that",
				msg.Reference(),
			)
			if sendErr != nil {
				h.logger.Error("failed to send non-guild context message", "error", sendErr)
			}
		}
		return false
	}

	if h.allowedGuildID != "" && msg.GuildID != h.allowedGuildID {
		h.logger.Info("ignoring command from unauthorized guild",
			"author_id", msg.Author.ID,
			"channel_id", msg.ChannelID,
			"guild_id", msg.GuildID,
		)
		// Send "I'm sorry Dave" message for unauthorized guild
		if s != nil {
			_, sendErr := s.ChannelMessageSendReply(
				msg.ChannelID,
				"I'm sorry Dave, I'm afraid I can't do that",
				msg.Reference(),
			)
			if sendErr != nil {
				h.logger.Error("failed to send unauthorized guild message", "error", sendErr)
			}
		}
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
		// Send "I'm sorry Dave" message for role resolution failure
		if s != nil {
			_, sendErr := s.ChannelMessageSendReply(
				msg.ChannelID,
				"I'm sorry Dave, I'm afraid I can't do that",
				msg.Reference(),
			)
			if sendErr != nil {
				h.logger.Error("failed to send role resolution failure message", "error", sendErr)
			}
		}
		return false
	}

	if !hasRole(roles, h.allowedRoleID) {
		h.logger.Info("ignoring command from user without required role",
			"author_id", msg.Author.ID,
			"guild_id", msg.GuildID,
		)
		// Send "I'm sorry Dave" message for lack of required role
		if s != nil {
			_, sendErr := s.ChannelMessageSendReply(
				msg.ChannelID,
				"I'm sorry Dave, I'm afraid I can't do that",
				msg.Reference(),
			)
			if sendErr != nil {
				h.logger.Error("failed to send role authorization message", "error", sendErr)
			}
		}
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
