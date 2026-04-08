// Package handler provides HTTP handlers for the github-webhook service.
package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/imdevinc/minions/github-webhook/internal/github"
	"github.com/imdevinc/minions/github-webhook/internal/orchestrator"
)

// Config holds webhook handler configuration.
type Config struct {
	WebhookSecret string
	BotUsername   string // e.g., "my-app[bot]"
	ApprovedRepos map[string]bool
	GitHubClient  *github.Client
	Orchestrator  *orchestrator.Client
	Logger        *slog.Logger
}

// WebhookHandler handles GitHub webhook events.
type WebhookHandler struct {
	webhookSecret string
	botUsername   string
	approvedRepos map[string]bool
	ghClient      *github.Client
	orchestrator  *orchestrator.Client
	logger        *slog.Logger
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(cfg Config) *WebhookHandler {
	return &WebhookHandler{
		webhookSecret: cfg.WebhookSecret,
		botUsername:   cfg.BotUsername,
		approvedRepos: cfg.ApprovedRepos,
		ghClient:      cfg.GitHubClient,
		orchestrator:  cfg.Orchestrator,
		logger:        cfg.Logger,
	}
}

// HandleWebhook processes incoming GitHub webhook events.
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read request body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Verify signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.verifySignature(body, signature) {
		h.logger.Warn("invalid webhook signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Get event type
	eventType := r.Header.Get("X-GitHub-Event")
	h.logger.Info("received webhook event", "event_type", eventType)

	// Handle based on event type
	ctx := r.Context()
	switch eventType {
	case "issue_comment":
		h.handleIssueComment(ctx, body)
	case "pull_request_review_comment":
		h.handlePRReviewComment(ctx, body)
	case "pull_request_review":
		h.handlePRReview(ctx, body)
	default:
		h.logger.Debug("ignoring event type", "event_type", eventType)
	}

	// Always return 200 to acknowledge receipt
	w.WriteHeader(http.StatusOK)
}

// verifySignature verifies the webhook payload signature.
func (h *WebhookHandler) verifySignature(payload []byte, signature string) bool {
	if signature == "" {
		return false
	}

	// Signature format: sha256=<hex>
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sigHex := strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sigHex), []byte(expected))
}

// IssueCommentEvent represents a GitHub issue_comment webhook payload.
type IssueCommentEvent struct {
	Action  string `json:"action"`
	Comment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		} `json:"user"`
	} `json:"comment"`
	Issue struct {
		Number      int `json:"number"`
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	} `json:"issue"`
	Repository struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
}

// PRReviewCommentEvent represents a GitHub pull_request_review_comment webhook payload.
type PRReviewCommentEvent struct {
	Action  string `json:"action"`
	Comment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		} `json:"user"`
	} `json:"comment"`
	PullRequest struct {
		Number int `json:"number"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
}

// PRReviewEvent represents a GitHub pull_request_review webhook payload.
type PRReviewEvent struct {
	Action string `json:"action"`
	Review struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		} `json:"user"`
	} `json:"review"`
	PullRequest struct {
		Number int `json:"number"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
}

func (h *WebhookHandler) handleIssueComment(ctx context.Context, body []byte) {
	var event IssueCommentEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error("failed to unmarshal issue_comment event", "error", err)
		return
	}

	// Only handle created comments
	if event.Action != "created" {
		return
	}

	// Only handle PR comments (not issue comments)
	if event.Issue.PullRequest == nil {
		h.logger.Debug("ignoring issue comment (not on PR)")
		return
	}

	h.processComment(ctx, commentInfo{
		repo:        event.Repository.FullName,
		owner:       event.Repository.Owner.Login,
		repoName:    event.Repository.Name,
		prNumber:    event.Issue.Number,
		commentID:   event.Comment.ID,
		commentType: "issue_comment",
		body:        event.Comment.Body,
		userLogin:   event.Comment.User.Login,
		userID:      event.Comment.User.ID,
	})
}

func (h *WebhookHandler) handlePRReviewComment(ctx context.Context, body []byte) {
	var event PRReviewCommentEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error("failed to unmarshal pull_request_review_comment event", "error", err)
		return
	}

	// Only handle created comments
	if event.Action != "created" {
		return
	}

	h.processComment(ctx, commentInfo{
		repo:        event.Repository.FullName,
		owner:       event.Repository.Owner.Login,
		repoName:    event.Repository.Name,
		prNumber:    event.PullRequest.Number,
		commentID:   event.Comment.ID,
		commentType: "pull_request_review_comment",
		body:        event.Comment.Body,
		userLogin:   event.Comment.User.Login,
		userID:      event.Comment.User.ID,
	})
}

func (h *WebhookHandler) handlePRReview(ctx context.Context, body []byte) {
	var event PRReviewEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error("failed to unmarshal pull_request_review event", "error", err)
		return
	}

	// Only handle submitted reviews
	if event.Action != "submitted" {
		return
	}

	// Skip reviews with empty body
	if strings.TrimSpace(event.Review.Body) == "" {
		h.logger.Debug("ignoring review with empty body")
		return
	}

	// For reviews, we can't react with emoji easily, so we'll just process
	// and comment if there's an issue
	h.processComment(ctx, commentInfo{
		repo:        event.Repository.FullName,
		owner:       event.Repository.Owner.Login,
		repoName:    event.Repository.Name,
		prNumber:    event.PullRequest.Number,
		commentID:   event.Review.ID,
		commentType: "pull_request_review", // Note: can't react to reviews the same way
		body:        event.Review.Body,
		userLogin:   event.Review.User.Login,
		userID:      event.Review.User.ID,
	})
}

type commentInfo struct {
	repo        string // owner/repo
	owner       string
	repoName    string
	prNumber    int
	commentID   int64
	commentType string
	body        string
	userLogin   string
	userID      int64
}

func (h *WebhookHandler) processComment(ctx context.Context, info commentInfo) {
	logger := h.logger.With(
		"repo", info.repo,
		"pr_number", info.prNumber,
		"comment_id", info.commentID,
		"user", info.userLogin,
	)

	// Check if repo is approved
	if !h.approvedRepos[info.repo] {
		logger.Debug("ignoring comment from unapproved repo")
		return
	}

	// Check for @mention of bot
	if !h.containsBotMention(info.body) {
		logger.Debug("ignoring comment without bot mention")
		return
	}

	logger.Info("processing comment with bot mention")

	// Construct PR URL
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", info.repo, info.prNumber)

	// Check if there's already an active minion for this PR
	activeMinion, err := h.orchestrator.GetActiveForPR(ctx, prURL)
	if err != nil {
		logger.Error("failed to check for active minion", "error", err)
		return
	}

	if activeMinion != nil {
		// Minion already running - react with ⏳ and comment
		logger.Info("minion already active for PR", "minion_id", activeMinion.ID)

		if info.commentType == "issue_comment" || info.commentType == "pull_request_review_comment" {
			_ = h.ghClient.ReactToComment(ctx, info.owner, info.repoName, info.commentID, info.commentType, "hourglass")
		}

		comment := fmt.Sprintf("🤖 A minion is already working on this PR. Please wait for it to finish before requesting more changes.\n\nActive minion: `%s` (status: %s)",
			activeMinion.ID, activeMinion.Status)
		_ = h.ghClient.CommentOnPR(ctx, info.owner, info.repoName, info.prNumber, comment)
		return
	}

	// React with 👀 to acknowledge
	if info.commentType == "issue_comment" || info.commentType == "pull_request_review_comment" {
		if err := h.ghClient.ReactToComment(ctx, info.owner, info.repoName, info.commentID, info.commentType, "eyes"); err != nil {
			logger.Error("failed to react to comment", "error", err)
			// Continue anyway - reaction is not critical
		}
	}

	// Get PR branch
	branch, err := h.ghClient.GetPRBranch(ctx, info.owner, info.repoName, info.prNumber)
	if err != nil {
		logger.Error("failed to get PR branch", "error", err)
		return
	}

	// Create minion
	req := orchestrator.CreateMinionRequest{
		Repo:            info.repo,
		Task:            info.body, // Use the comment body as the task
		Platform:        "github",
		Branch:          branch,
		SourcePRURL:     prURL,
		GitHubCommentID: strconv.FormatInt(info.commentID, 10),
		GitHubUserID:    strconv.FormatInt(info.userID, 10),
		GitHubUsername:  info.userLogin,
	}

	result, err := h.orchestrator.CreateMinion(ctx, req)
	if err != nil {
		logger.Error("failed to create minion", "error", err)
		return
	}

	logger.Info("minion created successfully", "minion_id", result.ID, "status", result.Status)
}

// containsBotMention checks if the body contains an @mention of the bot.
func (h *WebhookHandler) containsBotMention(body string) bool {
	// Look for @bot-name[bot] or @bot-name (case-insensitive)
	lowerBody := strings.ToLower(body)
	lowerBot := strings.ToLower(h.botUsername)

	// Check for @username format
	mention := "@" + lowerBot
	if strings.Contains(lowerBody, mention) {
		return true
	}

	// Also check without [bot] suffix in case user types it differently
	withoutBot := strings.TrimSuffix(lowerBot, "[bot]")
	if withoutBot != lowerBot && strings.Contains(lowerBody, "@"+withoutBot) {
		return true
	}

	return false
}
