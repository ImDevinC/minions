// Package config handles configuration loading for the github-webhook service.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the github-webhook service.
type Config struct {
	// Port for the HTTP server
	Port string

	// GitHub App credentials
	GitHubAppID         int64
	GitHubAppPrivateKey []byte

	// Webhook secret for payload verification
	GitHubWebhookSecret string

	// Orchestrator connection
	OrchestratorURL  string
	InternalAPIToken string

	// Approved repos (loaded from file)
	ApprovedRepos map[string]bool
}

// Load loads configuration from environment variables and files.
func Load() (*Config, error) {
	cfg := &Config{}

	// Port (optional, defaults to 8080)
	cfg.Port = os.Getenv("PORT")
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	// GitHub App ID
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		return nil, fmt.Errorf("GITHUB_APP_ID is required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("GITHUB_APP_ID must be a valid integer: %w", err)
	}
	cfg.GitHubAppID = appID

	// GitHub App Private Key
	privateKey := os.Getenv("GITHUB_APP_PRIVATE_KEY")
	if privateKey == "" {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY is required")
	}
	cfg.GitHubAppPrivateKey = []byte(privateKey)

	// Webhook Secret
	cfg.GitHubWebhookSecret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	if cfg.GitHubWebhookSecret == "" {
		return nil, fmt.Errorf("GITHUB_WEBHOOK_SECRET is required")
	}

	// Orchestrator URL
	cfg.OrchestratorURL = os.Getenv("ORCHESTRATOR_URL")
	if cfg.OrchestratorURL == "" {
		return nil, fmt.Errorf("ORCHESTRATOR_URL is required")
	}

	// Internal API Token
	cfg.InternalAPIToken = os.Getenv("INTERNAL_API_TOKEN")
	if cfg.InternalAPIToken == "" {
		return nil, fmt.Errorf("INTERNAL_API_TOKEN is required")
	}

	// Load approved repos from file
	reposPath := os.Getenv("APPROVED_REPOS_PATH")
	if reposPath == "" {
		reposPath = "/config/approved-repos.txt"
	}
	repos, err := loadApprovedRepos(reposPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load approved repos: %w", err)
	}
	cfg.ApprovedRepos = repos

	return cfg, nil
}

// loadApprovedRepos reads a file with one repo per line (owner/repo format).
func loadApprovedRepos(path string) (map[string]bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open approved repos file %s: %w", path, err)
	}
	defer file.Close()

	repos := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		repos[strings.ToLower(line)] = true
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading approved repos file: %w", err)
	}

	return repos, nil
}
