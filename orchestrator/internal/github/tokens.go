// Package github provides GitHub App token management for the orchestrator.
package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v84/github"
)

// ErrAccessRevoked is returned when the GitHub App installation has been revoked or suspended.
var ErrAccessRevoked = errors.New("GitHub access revoked")

// ErrInstallationNotFound is returned when no installation exists for a repository owner.
var ErrInstallationNotFound = errors.New("GitHub App not installed for this owner")

// TokenManager handles GitHub App installation token generation and caching.
// It automatically refreshes tokens before they expire (handled by ghinstallation).
type TokenManager interface {
	// GetToken returns an installation token for the given repository.
	// The repo format is "owner/name" (e.g., "imdevinc/minions").
	// Returns ErrAccessRevoked if the installation has been revoked/suspended.
	// Returns ErrInstallationNotFound if no installation exists for the owner.
	GetToken(ctx context.Context, repo string) (string, error)
}

// Config holds configuration for the GitHub App.
type Config struct {
	// AppID is the GitHub App ID.
	AppID int64

	// PrivateKey is the PEM-encoded private key for the app.
	PrivateKey []byte
}

// Manager implements TokenManager using ghinstallation/v2.
type Manager struct {
	appID      int64
	privateKey []byte
	logger     *slog.Logger

	// appTransport is used to make API calls as the app (not an installation).
	// This is needed to list installations and find the one for a given owner.
	appTransport *ghinstallation.AppsTransport

	// mu protects the transports map.
	mu sync.RWMutex

	// transports caches installation transports by installation ID.
	// ghinstallation handles token auto-refresh internally.
	transports map[int64]*ghinstallation.Transport
}

// NewManager creates a new GitHub App token manager.
func NewManager(cfg Config, logger *slog.Logger) (*Manager, error) {
	if cfg.AppID == 0 {
		return nil, fmt.Errorf("github app ID is required")
	}
	if len(cfg.PrivateKey) == 0 {
		return nil, fmt.Errorf("github app private key is required")
	}

	// Create app-level transport for listing installations.
	appTransport, err := ghinstallation.NewAppsTransport(
		http.DefaultTransport,
		cfg.AppID,
		cfg.PrivateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create app transport: %w", err)
	}

	return &Manager{
		appID:        cfg.AppID,
		privateKey:   cfg.PrivateKey,
		logger:       logger,
		appTransport: appTransport,
		transports:   make(map[int64]*ghinstallation.Transport),
	}, nil
}

// GetToken returns an installation token for the given repository.
// The repo format is "owner/name" (e.g., "imdevinc/minions").
func (m *Manager) GetToken(ctx context.Context, repo string) (string, error) {
	owner, _, ok := parseRepo(repo)
	if !ok {
		return "", fmt.Errorf("invalid repo format: %s", repo)
	}

	// Find installation for this owner.
	installationID, err := m.findInstallation(ctx, owner)
	if err != nil {
		return "", err
	}

	// Get or create transport for this installation.
	transport, err := m.getTransport(installationID)
	if err != nil {
		return "", err
	}

	// Get the token (ghinstallation handles refresh automatically).
	token, err := transport.Token(ctx)
	if err != nil {
		// Check for HTTP errors indicating revoked/suspended access.
		if isAccessRevokedError(err) {
			m.logger.Error("GitHub access revoked",
				"repo", repo,
				"installation_id", installationID,
				"error", err,
			)
			return "", fmt.Errorf("%w: %v", ErrAccessRevoked, err)
		}
		return "", fmt.Errorf("failed to get installation token: %w", err)
	}

	m.logger.Debug("got installation token",
		"repo", repo,
		"installation_id", installationID,
	)

	return token, nil
}

// findInstallation finds the installation ID for a given owner (user or org).
func (m *Manager) findInstallation(ctx context.Context, owner string) (int64, error) {
	client := github.NewClient(&http.Client{Transport: m.appTransport})

	// Try to get installation for the owner directly.
	// This works for both users and organizations.
	installation, resp, err := client.Apps.FindUserInstallation(ctx, owner)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			m.logger.Warn("no GitHub App installation for owner",
				"owner", owner,
			)
			return 0, ErrInstallationNotFound
		}
		if isAccessRevokedError(err) {
			return 0, fmt.Errorf("%w: %v", ErrAccessRevoked, err)
		}
		return 0, fmt.Errorf("failed to find installation for %s: %w", owner, err)
	}

	if installation.GetID() == 0 {
		return 0, ErrInstallationNotFound
	}

	m.logger.Debug("found installation",
		"owner", owner,
		"installation_id", installation.GetID(),
	)

	return installation.GetID(), nil
}

// getTransport returns a cached or new installation transport.
func (m *Manager) getTransport(installationID int64) (*ghinstallation.Transport, error) {
	// Check cache first.
	m.mu.RLock()
	transport, ok := m.transports[installationID]
	m.mu.RUnlock()
	if ok {
		return transport, nil
	}

	// Create new transport.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock.
	if transport, ok = m.transports[installationID]; ok {
		return transport, nil
	}

	transport, err := ghinstallation.New(
		http.DefaultTransport,
		m.appID,
		installationID,
		m.privateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create installation transport: %w", err)
	}

	m.transports[installationID] = transport
	m.logger.Debug("created installation transport",
		"installation_id", installationID,
	)

	return transport, nil
}

// parseRepo splits "owner/name" into owner and name.
func parseRepo(repo string) (owner, name string, ok bool) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// isAccessRevokedError checks if an error indicates revoked or suspended access.
func isAccessRevokedError(err error) bool {
	var httpErr *ghinstallation.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.Response != nil {
			switch httpErr.Response.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return true
			}
		}
	}

	// Also check for generic HTTP errors in the error chain.
	// go-github wraps errors differently sometimes.
	if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
		return true
	}

	return false
}

// NoOpTokenManager is a stub implementation for testing/non-GitHub environments.
type NoOpTokenManager struct {
	logger       *slog.Logger
	DefaultToken string
}

// NewNoOpTokenManager creates a no-op token manager.
// If defaultToken is provided, GetToken always returns it.
// If defaultToken is empty, GetToken returns an error.
func NewNoOpTokenManager(logger *slog.Logger, defaultToken string) *NoOpTokenManager {
	return &NoOpTokenManager{
		logger:       logger,
		DefaultToken: defaultToken,
	}
}

// GetToken returns the default token or an error.
func (m *NoOpTokenManager) GetToken(ctx context.Context, repo string) (string, error) {
	if m.DefaultToken != "" {
		if m.logger != nil {
			m.logger.Info("no-op token manager returning default token", "repo", repo)
		}
		return m.DefaultToken, nil
	}
	return "", fmt.Errorf("no GitHub token configured")
}
