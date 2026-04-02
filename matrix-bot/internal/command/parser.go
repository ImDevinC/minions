// Package command provides Matrix command parsing for minion mentions.
package command

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// Command validation limits
const (
	MaxTaskLength = 1 << 20 // 1MB
)

// Sentinel errors for validation failures
var (
	ErrMissingRepo       = errors.New("missing required --repo flag")
	ErrInvalidRepoFormat = errors.New("invalid repo format: expected Owner/Repo")
	ErrMissingTask       = errors.New("missing task description")
	ErrTaskTooLong       = errors.New("task exceeds maximum length of 1MB")
	ErrTaskHasControl    = errors.New("task contains invalid control characters")
)

// Command represents a parsed minion command
type Command struct {
	Repo  string // Owner/Repo format
	Model string // provider/model-name format
	Task  string // remaining text after flags
}

// repoRegex validates Owner/Repo format (supports nested paths)
var repoRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(/[a-zA-Z0-9_.-]+)*$`)

// Parse extracts a Command from a message mentioning the bot.
// The message should have the mention already stripped.
// Model is optional - orchestrator will apply default if empty.
func Parse(text string) (*Command, error) {
	// Extract --repo flag
	repo, text, err := extractFlag(text, "--repo")
	if err != nil {
		return nil, err
	}
	if repo == "" {
		return nil, ErrMissingRepo
	}
	if !repoRegex.MatchString(repo) {
		return nil, ErrInvalidRepoFormat
	}

	// Extract --model flag (optional, can be empty string)
	model, text, err := extractFlag(text, "--model")
	if err != nil {
		return nil, err
	}
	// Model validation is delegated to orchestrator

	// Remaining text is the task
	task := strings.TrimSpace(text)
	if task == "" {
		return nil, ErrMissingTask
	}
	if len(task) > MaxTaskLength {
		return nil, ErrTaskTooLong
	}
	if hasControlChars(task) {
		return nil, ErrTaskHasControl
	}

	return &Command{
		Repo:  repo,
		Model: model,
		Task:  task,
	}, nil
}

// extractFlag finds and removes a flag and its value from text.
// Returns (value, remaining_text, error).
// Supports both --flag value and --flag="value" or --flag='value' syntax.
func extractFlag(text, flag string) (string, string, error) {
	// Try quoted value first: --flag="value" or --flag='value'
	for _, quote := range []string{`"`, `'`} {
		pattern := regexp.MustCompile(regexp.QuoteMeta(flag) + `=` + quote + `([^` + quote + `]*)` + quote)
		if matches := pattern.FindStringSubmatch(text); len(matches) > 1 {
			value := matches[1]
			remaining := strings.TrimSpace(pattern.ReplaceAllString(text, ""))
			return value, remaining, nil
		}
	}

	// Try space-separated: --flag value (value ends at next -- or end of string)
	pattern := regexp.MustCompile(regexp.QuoteMeta(flag) + `\s+(\S+)`)
	if matches := pattern.FindStringSubmatch(text); len(matches) > 1 {
		value := matches[1]
		// Don't consume if value starts with -- (it's another flag)
		if strings.HasPrefix(value, "--") {
			return "", text, nil
		}
		remaining := strings.TrimSpace(pattern.ReplaceAllString(text, ""))
		return value, remaining, nil
	}

	// Flag not present
	return "", text, nil
}

// hasControlChars checks if text contains control characters (except newline, tab, carriage return)
func hasControlChars(text string) bool {
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return true
		}
	}
	return false
}

// StripMention removes the bot mention from the beginning of a message.
// In Matrix, mentions are typically in the form of a Matrix ID like @botname:server.com
// or an HTML pill like <a href="https://matrix.to/#/@botname:server.com">Botname</a>
func StripMention(content string, botUserID string) string {
	content = strings.TrimSpace(content)

	// Remove Matrix user ID mention (e.g., "@minion:matrix.org")
	if strings.HasPrefix(content, botUserID) {
		return strings.TrimSpace(strings.TrimPrefix(content, botUserID))
	}

	// Also try without the leading @
	if strings.HasPrefix(botUserID, "@") {
		withoutAt := botUserID[1:]
		if strings.HasPrefix(content, withoutAt) {
			return strings.TrimSpace(strings.TrimPrefix(content, withoutAt))
		}
	}

	// Remove colon after mention if present (common pattern: "@bot: do something")
	if strings.HasPrefix(content, ":") {
		content = strings.TrimSpace(strings.TrimPrefix(content, ":"))
	}

	return content
}

// IsMentioned checks if the message mentions the bot.
// In Matrix, this checks for the bot's user ID in the message body.
func IsMentioned(content string, botUserID string) bool {
	return strings.Contains(content, botUserID)
}
