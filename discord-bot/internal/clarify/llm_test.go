package clarify

import "testing"

func TestParseClarificationResponse(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		wantReady    bool
		wantQuestion string
	}{
		{
			name:      "ready response",
			text:      "  READY  ",
			wantReady: true,
		},
		{
			name:         "normal clarification question",
			text:         "What specific endpoint should this apply to?",
			wantReady:    false,
			wantQuestion: "What specific endpoint should this apply to?",
		},
		{
			name:      "codebase structure question is converted to ready",
			text:      "Which file contains the handler that needs to be updated?",
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := parseClarificationResponse(tt.text)
			if resp.Ready != tt.wantReady {
				t.Errorf("Ready = %v, want %v", resp.Ready, tt.wantReady)
			}
			if resp.Question != tt.wantQuestion {
				t.Errorf("Question = %q, want %q", resp.Question, tt.wantQuestion)
			}
		})
	}
}

func TestIsCodebaseStructureQuestion(t *testing.T) {
	tests := []struct {
		name     string
		question string
		want     bool
	}{
		{
			name:     "asks for file",
			question: "What file should I edit for this change?",
			want:     true,
		},
		{
			name:     "asks for directory",
			question: "Which directory contains this implementation?",
			want:     true,
		},
		{
			name:     "asks behavior requirement",
			question: "Should this include retries for 5xx responses?",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCodebaseStructureQuestion(tt.question)
			if got != tt.want {
				t.Errorf("isCodebaseStructureQuestion() = %v, want %v", got, tt.want)
			}
		})
	}
}
