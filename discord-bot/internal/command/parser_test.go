package command

import (
	"errors"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Command
		wantErr error
	}{
		{
			name:  "valid command with defaults",
			input: "--repo owner/repo Fix the bug in main.go",
			want: &Command{
				Repo:  "owner/repo",
				Model: DefaultModel,
				Task:  "Fix the bug in main.go",
			},
		},
		{
			name:  "valid command with explicit model",
			input: "--repo owner/repo --model openai/gpt-4 Add a README",
			want: &Command{
				Repo:  "owner/repo",
				Model: "openai/gpt-4",
				Task:  "Add a README",
			},
		},
		{
			name:  "model before repo",
			input: "--model anthropic/claude-3-opus --repo myorg/myproject Refactor utils",
			want: &Command{
				Repo:  "myorg/myproject",
				Model: "anthropic/claude-3-opus",
				Task:  "Refactor utils",
			},
		},
		{
			name:  "nested repo path",
			input: "--repo org/team/project Do the thing",
			want: &Command{
				Repo:  "org/team/project",
				Model: DefaultModel,
				Task:  "Do the thing",
			},
		},
		{
			name:  "repo with special chars",
			input: "--repo my_org.name/my-repo_v2 Add tests",
			want: &Command{
				Repo:  "my_org.name/my-repo_v2",
				Model: DefaultModel,
				Task:  "Add tests",
			},
		},
		{
			name:  "quoted repo value",
			input: `--repo="owner/repo" Fix it`,
			want: &Command{
				Repo:  "owner/repo",
				Model: DefaultModel,
				Task:  "Fix it",
			},
		},
		{
			name:  "single quoted values",
			input: `--repo='owner/repo' --model='anthropic/claude' Do it`,
			want: &Command{
				Repo:  "owner/repo",
				Model: "anthropic/claude",
				Task:  "Do it",
			},
		},
		{
			name:  "multiline task",
			input: "--repo owner/repo Fix the following:\n1. Bug A\n2. Bug B",
			want: &Command{
				Repo:  "owner/repo",
				Model: DefaultModel,
				Task:  "Fix the following:\n1. Bug A\n2. Bug B",
			},
		},
		{
			name:    "missing repo",
			input:   "--model anthropic/claude Fix it",
			wantErr: ErrMissingRepo,
		},
		{
			name:    "invalid repo format - no slash",
			input:   "--repo ownerrepo Fix it",
			wantErr: ErrInvalidRepoFormat,
		},
		{
			name:    "invalid repo format - just slash",
			input:   "--repo / Fix it",
			wantErr: ErrInvalidRepoFormat,
		},
		{
			name:    "empty task",
			input:   "--repo owner/repo   ",
			wantErr: ErrMissingTask,
		},
		{
			name:    "task too long",
			input:   "--repo owner/repo " + strings.Repeat("a", MaxTaskLength+1),
			wantErr: ErrTaskTooLong,
		},
		{
			name:    "task with control characters",
			input:   "--repo owner/repo Fix the \x00 bug",
			wantErr: ErrTaskHasControl,
		},
		{
			name:    "unknown model provider",
			input:   "--repo owner/repo --model google/gemini Do it",
			wantErr: ErrUnknownModel,
		},
		{
			name:    "invalid model format",
			input:   "--repo owner/repo --model claude Do it",
			wantErr: ErrUnknownModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Repo != tt.want.Repo {
				t.Errorf("Repo = %q, want %q", got.Repo, tt.want.Repo)
			}
			if got.Model != tt.want.Model {
				t.Errorf("Model = %q, want %q", got.Model, tt.want.Model)
			}
			if got.Task != tt.want.Task {
				t.Errorf("Task = %q, want %q", got.Task, tt.want.Task)
			}
		})
	}
}

func TestStripMention(t *testing.T) {
	botID := "123456789"

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "standard mention at start",
			content: "<@123456789> --repo owner/repo Fix it",
			want:    "--repo owner/repo Fix it",
		},
		{
			name:    "nickname mention at start",
			content: "<@!123456789> --repo owner/repo Fix it",
			want:    "--repo owner/repo Fix it",
		},
		{
			name:    "mention with extra spaces",
			content: "  <@123456789>   --repo owner/repo Fix it  ",
			want:    "--repo owner/repo Fix it",
		},
		{
			name:    "wrong bot ID",
			content: "<@999999999> --repo owner/repo Fix it",
			want:    "<@999999999> --repo owner/repo Fix it",
		},
		{
			name:    "no mention",
			content: "--repo owner/repo Fix it",
			want:    "--repo owner/repo Fix it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMention(tt.content, botID)
			if got != tt.want {
				t.Errorf("StripMention() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsMentioned(t *testing.T) {
	botID := "123456789"

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "standard mention",
			content: "<@123456789> hello",
			want:    true,
		},
		{
			name:    "nickname mention",
			content: "<@!123456789> hello",
			want:    true,
		},
		{
			name:    "mention in middle",
			content: "hey <@123456789> do this",
			want:    true,
		},
		{
			name:    "wrong ID",
			content: "<@999999999> hello",
			want:    false,
		},
		{
			name:    "no mention",
			content: "just a normal message",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMentioned(tt.content, botID)
			if got != tt.want {
				t.Errorf("IsMentioned() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasControlChars(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"normal text", "Hello world", false},
		{"with newline", "Hello\nworld", false},
		{"with tab", "Hello\tworld", false},
		{"with carriage return", "Hello\rworld", false},
		{"with null byte", "Hello\x00world", true},
		{"with bell", "Hello\x07world", true},
		{"with form feed", "Hello\x0cworld", true},
		{"with escape", "Hello\x1bworld", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasControlChars(tt.text)
			if got != tt.want {
				t.Errorf("hasControlChars() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAllowedModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"anthropic/claude-3-opus", true},
		{"anthropic/claude-sonnet-4-5", true},
		{"openai/gpt-4", true},
		{"openai/gpt-4-turbo", true},
		{"google/gemini", false},
		{"claude", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := isAllowedModel(tt.model)
			if got != tt.want {
				t.Errorf("isAllowedModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}
