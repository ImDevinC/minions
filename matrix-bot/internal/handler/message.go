// Package handler provides Matrix message handlers for the minion bot.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"

	"github.com/imdevinc/minions/matrix-bot/internal/clarify"
	"github.com/imdevinc/minions/matrix-bot/internal/command"
	"github.com/imdevinc/minions/matrix-bot/internal/orchestrator"
)

// ThinkingReaction is the reaction added when processing a command
const ThinkingReaction = "🤔"

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

// ClarificationLookup looks up minions by clarification event ID.
type ClarificationLookup interface {
	GetByMatrixClarificationEventID(ctx context.Context, eventID string) (*orchestrator.MinionByClarificationResponse, error)
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

// MessageHandler handles incoming Matrix messages
type MessageHandler struct {
	logger          *slog.Logger
	client          *mautrix.Client
	orchestrator    Orchestrator
	clarification   ClarificationEvaluator
	botUserID       id.UserID
	allowedRooms    map[id.RoomID]bool // nil means all rooms allowed
	allowedUsers    map[id.UserID]bool // nil means all users allowed
	controlPanelURL string
	startupTime     int64 // Unix milliseconds, events before this are ignored
}

// NewMessageHandler creates a new message handler.
// startupTime is the Unix milliseconds timestamp when the bot started; events older than
// this are ignored to prevent reprocessing on restart.
func NewMessageHandler(
	logger *slog.Logger,
	client *mautrix.Client,
	orch Orchestrator,
	clarification ClarificationEvaluator,
	botUserID id.UserID,
	allowedRooms []id.RoomID,
	allowedUsers []id.UserID,
	controlPanelURL string,
	startupTime int64,
) *MessageHandler {
	var roomMap map[id.RoomID]bool
	if len(allowedRooms) > 0 {
		roomMap = make(map[id.RoomID]bool)
		for _, r := range allowedRooms {
			roomMap[r] = true
		}
	}

	var userMap map[id.UserID]bool
	if len(allowedUsers) > 0 {
		userMap = make(map[id.UserID]bool)
		for _, u := range allowedUsers {
			userMap[u] = true
		}
	}

	return &MessageHandler{
		logger:          logger,
		client:          client,
		orchestrator:    orch,
		clarification:   clarification,
		botUserID:       botUserID,
		allowedRooms:    roomMap,
		allowedUsers:    userMap,
		controlPanelURL: controlPanelURL,
		startupTime:     startupTime,
	}
}

// Handle processes a Matrix message event
func (h *MessageHandler) Handle(ctx context.Context, evt *event.Event) {
	// Only process room messages
	if evt.Type != event.EventMessage {
		return
	}

	// Ignore messages from ourselves
	if evt.Sender == h.botUserID {
		return
	}

	// Ignore messages from before bot startup to prevent reprocessing on restart
	if evt.Timestamp < h.startupTime {
		return
	}

	// Check room restrictions
	if !h.isRoomAllowed(evt.RoomID) {
		return
	}

	// Check user restrictions
	if !h.isUserAllowed(evt.Sender) {
		return
	}

	content := evt.Content.AsMessage()
	if content == nil {
		return
	}

	// Get the body text (use formatted body if available, fall back to plain)
	body := content.Body
	if body == "" {
		return
	}

	// Check if we're mentioned
	if !h.isMentioned(body, content) {
		return
	}

	h.logger.Info("received mention",
		"sender", evt.Sender,
		"room_id", evt.RoomID,
		"event_id", evt.ID,
	)

	// Add thinking reaction to acknowledge
	h.addReaction(evt.RoomID, evt.ID, ThinkingReaction)
	defer h.removeReaction(evt.RoomID, evt.ID, ThinkingReaction)

	// Strip the mention and parse the command
	text := h.stripMention(body)
	cmd, err := command.Parse(text)
	if err != nil {
		h.handleParseError(ctx, evt.RoomID, evt.ID, err)
		return
	}

	h.logger.Info("parsed command",
		"repo", cmd.Repo,
		"model", cmd.Model,
		"task_length", len(cmd.Task),
		"sender", evt.Sender,
		"event_id", evt.ID,
	)

	// Create timeout context for clarification evaluation
	evalCtx, evalCancel := context.WithTimeout(ctx, OperationTimeout)
	defer evalCancel()

	// Evaluate clarification first
	result, err := h.clarification.EvaluateWithRetry(evalCtx, cmd.Repo, cmd.Task)
	if err != nil {
		h.logger.Error("clarification LLM failed after retries",
			"error", err,
			"repo", cmd.Repo,
		)
		h.sendReply(evt.RoomID, evt.ID, "Failed to evaluate task clarity. Please try again.")
		return
	}

	if result.Ready {
		// Create timeout context for minion creation
		createCtx, createCancel := context.WithTimeout(ctx, OperationTimeout)
		defer createCancel()

		// Create minion in pending state (spawner will pick it up)
		resp, createErr := h.orchestrator.CreateMinion(createCtx, orchestrator.CreateMinionRequest{
			Repo:          cmd.Repo,
			Task:          cmd.Task,
			Model:         cmd.Model,
			InitialStatus: "pending",
			MatrixEventID: string(evt.ID),
			MatrixRoomID:  string(evt.RoomID),
			MatrixUserID:  string(evt.Sender),
		})
		if createErr != nil {
			h.handleOrchestratorError(ctx, evt.RoomID, evt.ID, createErr)
			return
		}

		if resp.Duplicate {
			h.logger.Info("duplicate minion detected",
				"minion_id", resp.ID,
				"repo", cmd.Repo,
			)
			h.sendReply(evt.RoomID, evt.ID, "A minion is already working on this task. Check the existing one!")
			return
		}

		h.logger.Info("task is ready, minion created",
			"minion_id", resp.ID,
			"repo", cmd.Repo,
		)
		msg := "Task is clear! Your minion is being spawned..."
		if h.controlPanelURL != "" {
			msg += fmt.Sprintf("\nView progress: %s/minions/%s", h.controlPanelURL, resp.ID)
		}
		h.sendReply(evt.RoomID, evt.ID, msg)
		return
	}

	// Task needs clarification - ask question first, then create minion in awaiting_clarification
	clarificationMsg := "**Clarification needed:** " + result.Question + "\n\n*Reply to this message with your answer.*"
	clarificationEventID := h.sendReply(evt.RoomID, evt.ID, clarificationMsg)
	if clarificationEventID == "" {
		h.logger.Error("failed to send clarification question",
			"repo", cmd.Repo,
		)
		h.sendReply(evt.RoomID, evt.ID, "Failed to ask clarification question. Please try again.")
		return
	}

	// Create timeout context for minion creation with clarification
	clarifyCtx, clarifyCancel := context.WithTimeout(ctx, OperationTimeout)
	defer clarifyCancel()

	resp, createErr := h.orchestrator.CreateMinion(clarifyCtx, orchestrator.CreateMinionRequest{
		Repo:                       cmd.Repo,
		Task:                       cmd.Task,
		Model:                      cmd.Model,
		InitialStatus:              "awaiting_clarification",
		ClarificationQuestion:      result.Question,
		MatrixClarificationEventID: string(clarificationEventID),
		MatrixEventID:              string(evt.ID),
		MatrixRoomID:               string(evt.RoomID),
		MatrixUserID:               string(evt.Sender),
	})
	if createErr != nil {
		// Try to redact the clarification message on failure
		h.redactEvent(evt.RoomID, id.EventID(clarificationEventID))
		h.handleOrchestratorError(ctx, evt.RoomID, evt.ID, createErr)
		return
	}

	if resp.Duplicate {
		// Clean up question we just posted; existing minion already handles this task
		h.redactEvent(evt.RoomID, id.EventID(clarificationEventID))
		h.logger.Info("duplicate minion detected during clarification",
			"minion_id", resp.ID,
			"repo", cmd.Repo,
		)
		h.sendReply(evt.RoomID, evt.ID, "A minion is already working on this task. Check the existing one!")
		return
	}

	h.logger.Info("clarification question posted and minion created",
		"minion_id", resp.ID,
		"repo", cmd.Repo,
		"clarification_event_id", clarificationEventID,
	)
}

// HandleReply processes a Matrix message that is a reply to another message.
// If the replied-to message is a clarification question, it processes the answer.
func (h *MessageHandler) HandleReply(ctx context.Context, evt *event.Event) {
	// Ignore messages from ourselves
	if evt.Sender == h.botUserID {
		h.logger.Debug("HandleReply: ignoring own message", "event_id", evt.ID)
		return
	}

	// Ignore messages from before bot startup to prevent reprocessing on restart
	if evt.Timestamp < h.startupTime {
		h.logger.Debug("HandleReply: ignoring old message",
			"event_id", evt.ID,
			"event_timestamp", evt.Timestamp,
			"startup_time", h.startupTime,
		)
		return
	}

	// Check room restrictions
	if !h.isRoomAllowed(evt.RoomID) {
		h.logger.Debug("HandleReply: room not allowed",
			"event_id", evt.ID,
			"room_id", evt.RoomID,
		)
		return
	}

	// Check user restrictions
	if !h.isUserAllowed(evt.Sender) {
		h.logger.Debug("HandleReply: user not allowed",
			"event_id", evt.ID,
			"sender", evt.Sender,
		)
		return
	}

	content := evt.Content.AsMessage()
	if content == nil {
		h.logger.Debug("HandleReply: nil message content",
			"event_id", evt.ID,
			"event_type", evt.Type,
		)
		return
	}

	// Check if this is a reply to another message
	// GetReplyTo() handles both simple replies and threaded replies,
	// returning the event ID the user clicked "reply" on
	replyToEventID := content.RelatesTo.GetReplyTo()
	if replyToEventID == "" {
		h.logger.Debug("HandleReply: not a reply message",
			"event_id", evt.ID,
			"sender", evt.Sender,
			"has_relates_to", content.RelatesTo != nil,
		)
		return
	}

	// Create timeout context for orchestrator calls
	opCtx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()

	h.logger.Debug("HandleReply: looking up clarification",
		"event_id", evt.ID,
		"reply_to_event_id", replyToEventID,
	)

	// Look up minion by the referenced event ID (could be a clarification question)
	minion, err := h.orchestrator.GetByMatrixClarificationEventID(opCtx, string(replyToEventID))
	if err != nil {
		if errors.Is(err, orchestrator.ErrClarificationNotFound) {
			// Not a clarification message, ignore silently - this is expected for most replies
			h.logger.Debug("HandleReply: not a clarification message",
				"event_id", evt.ID,
				"reply_to_event_id", replyToEventID,
			)
			return
		}
		h.logger.Error("failed to look up clarification",
			"error", err,
			"reply_to_event_id", replyToEventID,
			"sender", evt.Sender,
			"room_id", evt.RoomID,
		)
		return
	}

	h.logger.Info("received clarification reply",
		"minion_id", minion.ID,
		"sender", evt.Sender,
		"room_id", evt.RoomID,
	)

	// Check if minion is still awaiting clarification
	if minion.Status != "awaiting_clarification" {
		h.logger.Warn("minion not awaiting clarification",
			"minion_id", minion.ID,
			"status", minion.Status,
		)
		h.sendReply(evt.RoomID, evt.ID, "This minion is no longer waiting for clarification (status: "+minion.Status+")")
		return
	}

	// Validate that the reply is from the original requester
	if minion.MatrixUserID != string(evt.Sender) {
		h.logger.Warn("clarification reply from wrong user",
			"minion_id", minion.ID,
			"expected_user", minion.MatrixUserID,
			"actual_user", evt.Sender,
		)
		h.sendReply(evt.RoomID, evt.ID, "Only the original requester can answer clarification questions.")
		return
	}

	// Add thinking reaction to acknowledge
	h.addReaction(evt.RoomID, evt.ID, ThinkingReaction)
	defer h.removeReaction(evt.RoomID, evt.ID, ThinkingReaction)

	// Get the answer (the content of the reply)
	answer := h.extractReplyBody(content)
	if answer == "" {
		h.sendReply(evt.RoomID, evt.ID, "Your reply is empty. Please provide an answer to the clarification question.")
		return
	}

	// Set the clarification answer and transition minion back to pending
	err = h.orchestrator.SetClarificationAnswer(opCtx, minion.ID, answer)
	if err != nil {
		if errors.Is(err, orchestrator.ErrClarificationNotFound) {
			h.sendReply(evt.RoomID, evt.ID, "The minion for this clarification no longer exists.")
			return
		}
		h.logger.Error("failed to set clarification answer",
			"error", err,
			"minion_id", minion.ID,
		)
		h.sendReply(evt.RoomID, evt.ID, "Failed to process your answer. Please try again or contact support.")
		return
	}

	h.logger.Info("clarification answer accepted",
		"minion_id", minion.ID,
		"answer_length", len(answer),
	)

	// Confirm to the user
	msg := "Got it! Your minion is now being spawned with the clarified task..."
	if h.controlPanelURL != "" {
		msg += fmt.Sprintf("\nView progress: %s/minions/%s", h.controlPanelURL, minion.ID)
	}
	h.sendReply(evt.RoomID, evt.ID, msg)
}

// isRoomAllowed checks if the room is in the allowed list (or if all rooms are allowed)
func (h *MessageHandler) isRoomAllowed(roomID id.RoomID) bool {
	if h.allowedRooms == nil {
		return true
	}
	return h.allowedRooms[roomID]
}

// isUserAllowed checks if the user is in the allowed list (or if all users are allowed)
func (h *MessageHandler) isUserAllowed(userID id.UserID) bool {
	if h.allowedUsers == nil {
		return true
	}
	return h.allowedUsers[userID]
}

// isMentioned checks if the bot is mentioned in the message
func (h *MessageHandler) isMentioned(body string, content *event.MessageEventContent) bool {
	// Check for user ID mention in body
	if strings.Contains(body, string(h.botUserID)) {
		return true
	}

	// Check for local part of user ID (e.g., "@minion" from "@minion:matrix.org")
	localPart := h.botUserID.Localpart()
	if strings.Contains(strings.ToLower(body), "@"+strings.ToLower(localPart)) {
		return true
	}

	// Also check the formatted body for HTML pills
	if content.FormattedBody != "" {
		if strings.Contains(content.FormattedBody, string(h.botUserID)) {
			return true
		}
	}

	return false
}

// stripMention removes the bot mention from the message
func (h *MessageHandler) stripMention(body string) string {
	// Remove full user ID mention
	body = strings.ReplaceAll(body, string(h.botUserID), "")

	// Remove local part mention (e.g., "@minion:" or "@minion ")
	localPart := h.botUserID.Localpart()
	body = strings.ReplaceAll(body, "@"+localPart+":", "")
	body = strings.ReplaceAll(body, "@"+localPart+" ", " ")
	body = strings.ReplaceAll(body, "@"+localPart, "")

	// Remove any leading colon and whitespace
	body = strings.TrimLeft(body, ": \t")

	return strings.TrimSpace(body)
}

// extractReplyBody extracts the actual reply content, stripping the quoted original message
func (h *MessageHandler) extractReplyBody(content *event.MessageEventContent) string {
	body := content.Body

	// Matrix replies often include the quoted message in the body with "> " prefix
	// Remove all lines starting with "> " until we hit a non-quoted line
	lines := strings.Split(body, "\n")
	var replyLines []string
	foundContent := false
	for _, line := range lines {
		if !foundContent && strings.HasPrefix(line, "> ") {
			continue
		}
		// Skip empty lines after quotes before content
		if !foundContent && strings.TrimSpace(line) == "" {
			continue
		}
		foundContent = true
		replyLines = append(replyLines, line)
	}

	return strings.TrimSpace(strings.Join(replyLines, "\n"))
}

// sendReply sends a text message as a reply to the given event
func (h *MessageHandler) sendReply(roomID id.RoomID, replyTo id.EventID, text string) string {
	content := format.RenderMarkdown(text, true, false)
	content.RelatesTo = &event.RelatesTo{
		InReplyTo: &event.InReplyTo{
			EventID: replyTo,
		},
	}

	resp, err := h.client.SendMessageEvent(context.Background(), roomID, event.EventMessage, &content)
	if err != nil {
		h.logger.Error("failed to send reply",
			"error", err,
			"room_id", roomID,
			"reply_to", replyTo,
		)
		return ""
	}
	return string(resp.EventID)
}

// addReaction adds a reaction emoji to a message
func (h *MessageHandler) addReaction(roomID id.RoomID, eventID id.EventID, emoji string) {
	content := &event.ReactionEventContent{
		RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: eventID,
			Key:     emoji,
		},
	}

	_, err := h.client.SendMessageEvent(context.Background(), roomID, event.EventReaction, content)
	if err != nil {
		h.logger.Error("failed to add reaction",
			"error", err,
			"room_id", roomID,
			"event_id", eventID,
		)
	}
}

// removeReaction removes a reaction (by redacting it - Matrix doesn't have a proper remove reaction API)
func (h *MessageHandler) removeReaction(_ id.RoomID, _ id.EventID, _ string) {
	// Matrix doesn't have a standard way to remove reactions without knowing the reaction event ID
	// For now, we'll just leave the reaction. This is a known limitation.
	// In a production system, you'd track reaction event IDs or use a workaround.
}

// redactEvent redacts (deletes) an event
func (h *MessageHandler) redactEvent(roomID id.RoomID, eventID id.EventID) {
	_, err := h.client.RedactEvent(context.Background(), roomID, eventID)
	if err != nil {
		h.logger.Error("failed to redact event",
			"error", err,
			"room_id", roomID,
			"event_id", eventID,
		)
	}
}

// handleParseError sends an error message for parse failures
func (h *MessageHandler) handleParseError(_ context.Context, roomID id.RoomID, eventID id.EventID, err error) {
	h.logger.Warn("command parse failed",
		"error", err,
		"event_id", eventID,
	)

	// Build a user-friendly error message
	var msg string
	switch {
	case errors.Is(err, command.ErrMissingRepo):
		msg = "Missing `--repo` flag. Usage: `@minion --repo Owner/Repo <task>`"
	case errors.Is(err, command.ErrInvalidRepoFormat):
		msg = "Invalid repo format. Expected `Owner/Repo` (e.g., `octocat/hello-world`)"
	case errors.Is(err, command.ErrMissingTask):
		msg = "Missing task description. What should I do?"
	case errors.Is(err, command.ErrTaskTooLong):
		msg = "Task is too long (max 10,000 characters)"
	case errors.Is(err, command.ErrTaskHasControl):
		msg = "Task contains invalid characters"
	default:
		msg = "Failed to parse command: " + err.Error()
	}

	h.sendReply(roomID, eventID, msg)
}

// handleOrchestratorError sends an error message for orchestrator failures
func (h *MessageHandler) handleOrchestratorError(_ context.Context, roomID id.RoomID, eventID id.EventID, err error) {
	h.logger.Warn("orchestrator request failed",
		"error", err,
		"event_id", eventID,
	)

	// Build a user-friendly error message
	var msg string
	switch {
	case errors.Is(err, orchestrator.ErrRateLimitExceeded):
		msg = "You've hit the hourly limit (10 minions/hour). Please wait a bit before spawning more."
	case errors.Is(err, orchestrator.ErrConcurrentLimitExceeded):
		msg = "You have too many minions running (max 3). Wait for some to finish!"
	default:
		msg = "Failed to create minion: " + err.Error()
	}

	h.sendReply(roomID, eventID, msg)
}
