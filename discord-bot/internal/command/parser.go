// Package command provides Discord command parsing for minion mentions.
package command

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Command validation limits
const (
	MaxTaskLength = 10000
)

// Allowed model prefixes
var allowedModelPrefixes = []string{"anthropic/", "openai/"}

// DefaultModel is used when no --model flag is provided
const DefaultModel = "anthropic/claude-sonnet-4-5"

// Sentinel errors for validation failures
var (
	ErrMissingRepo       = errors.New("missing required --repo flag")
	ErrInvalidRepoFormat = errors.New("invalid repo format: expected Owner/Repo")
	ErrMissingTask       = errors.New("missing task description")
	ErrTaskTooLong       = errors.New("task exceeds maximum length of 10000 characters")
	ErrTaskHasControl    = errors.New("task contains invalid control characters")
	ErrUnknownModel      = errors.New("unknown model: must be anthropic/* or openai/*")
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

	// Extract --model flag (optional)
	model, text, err := extractFlag(text, "--model")
	if err != nil {
		return nil, err
	}
	if model == "" {
		model = DefaultModel
	} else {
		if !isAllowedModel(model) {
			return nil, fmt.Errorf("%w: %s", ErrUnknownModel, model)
		}
	}

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

// isAllowedModel checks if a model matches the allowed prefixes
func isAllowedModel(model string) bool {
	for _, prefix := range allowedModelPrefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
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
// Expects format like "<@123456789> rest of message" or "<@!123456789> rest".
func StripMention(content string, botUserID string) string {
	// Discord mentions: <@USER_ID> or <@!USER_ID> (nickname mention)
	mention := "<@" + botUserID + ">"
	nickMention := "<@!" + botUserID + ">"

	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, mention) {
		return strings.TrimSpace(strings.TrimPrefix(content, mention))
	}
	if strings.HasPrefix(content, nickMention) {
		return strings.TrimSpace(strings.TrimPrefix(content, nickMention))
	}
	return content
}

// IsMentioned checks if the message mentions the bot
func IsMentioned(content string, botUserID string) bool {
	mention := "<@" + botUserID + ">"
	nickMention := "<@!" + botUserID + ">"
	return strings.Contains(content, mention) || strings.Contains(content, nickMention)
}
