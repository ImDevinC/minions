// Package github provides a GitHub API client for the github-webhook service.
package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v84/github"
)

// Config holds GitHub client configuration.
type Config struct {
	AppID      int64
	PrivateKey []byte
}

// Client wraps the GitHub API client with methods needed for webhook handling.
type Client struct {
	appID      int64
	privateKey []byte
	logger     *slog.Logger
}

// NewClient creates a new GitHub client.
func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	return &Client{
		appID:      cfg.AppID,
		privateKey: cfg.PrivateKey,
		logger:     logger,
	}, nil
}

// GetBotUsername returns the bot username (app-slug[bot]) for @mention detection.
func (c *Client) GetBotUsername(ctx context.Context) (string, error) {
	// Create JWT transport for app-level API access
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, c.appID, c.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to create apps transport: %w", err)
	}

	client := gh.NewClient(&http.Client{Transport: transport})

	// Get app info
	app, _, err := client.Apps.Get(ctx, "")
	if err != nil {
		return "", fmt.Errorf("failed to get app info: %w", err)
	}

	if app.Slug == nil {
		return "", fmt.Errorf("app slug is nil")
	}

	// Bot username format: {slug}[bot]
	return *app.Slug + "[bot]", nil
}

// getInstallationClient creates a client authenticated as an installation for a specific repo.
func (c *Client) getInstallationClient(ctx context.Context, owner string) (*gh.Client, error) {
	// Create JWT transport for app-level API access
	appsTransport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, c.appID, c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create apps transport: %w", err)
	}

	appsClient := gh.NewClient(&http.Client{Transport: appsTransport})

	// Find installation for the owner
	installation, _, err := appsClient.Apps.FindUserInstallation(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to find installation for owner %s: %w", owner, err)
	}

	// Create installation transport
	installTransport, err := ghinstallation.New(http.DefaultTransport, c.appID, installation.GetID(), c.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create installation transport: %w", err)
	}

	return gh.NewClient(&http.Client{Transport: installTransport}), nil
}

// ReactToComment adds an emoji reaction to a comment.
// commentType should be "issue_comment" or "pull_request_review_comment".
func (c *Client) ReactToComment(ctx context.Context, owner, repo string, commentID int64, commentType, reaction string) error {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return err
	}

	switch commentType {
	case "issue_comment":
		_, _, err = client.Reactions.CreateIssueCommentReaction(ctx, owner, repo, commentID, reaction)
	case "pull_request_review_comment":
		_, _, err = client.Reactions.CreatePullRequestCommentReaction(ctx, owner, repo, commentID, reaction)
	default:
		return fmt.Errorf("unknown comment type: %s", commentType)
	}

	if err != nil {
		return fmt.Errorf("failed to create reaction: %w", err)
	}

	c.logger.Info("added reaction to comment",
		"owner", owner,
		"repo", repo,
		"comment_id", commentID,
		"reaction", reaction,
	)

	return nil
}

// CommentOnPR posts a comment on a pull request.
func (c *Client) CommentOnPR(ctx context.Context, owner, repo string, prNumber int, body string) error {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return err
	}

	comment := &gh.IssueComment{Body: gh.Ptr(body)}
	_, _, err = client.Issues.CreateComment(ctx, owner, repo, prNumber, comment)
	if err != nil {
		return fmt.Errorf("failed to create PR comment: %w", err)
	}

	c.logger.Info("posted comment on PR",
		"owner", owner,
		"repo", repo,
		"pr_number", prNumber,
	)

	return nil
}

// GetPRBranch gets the head branch name for a pull request.
func (c *Client) GetPRBranch(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return "", err
	}

	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return "", fmt.Errorf("failed to get PR: %w", err)
	}

	if pr.Head == nil || pr.Head.Ref == nil {
		return "", fmt.Errorf("PR head ref is nil")
	}

	return *pr.Head.Ref, nil
}

// GetUserIDFromLogin gets the GitHub user ID from a username.
func (c *Client) GetUserIDFromLogin(ctx context.Context, owner, login string) (string, error) {
	client, err := c.getInstallationClient(ctx, owner)
	if err != nil {
		return "", err
	}

	user, _, err := client.Users.Get(ctx, login)
	if err != nil {
		return "", fmt.Errorf("failed to get user: %w", err)
	}

	return strconv.FormatInt(user.GetID(), 10), nil
}
