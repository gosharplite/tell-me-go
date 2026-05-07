// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
)

type mockSkillSelector struct {
	selected []skills.Skill
	err      error
}

func (m *mockSkillSelector) SelectSkills(ctx context.Context, taskDescription string) ([]skills.Skill, error) {
	return m.selected, m.err
}

// mockLogger records Warn calls for assertion in tests.
type mockLogger struct {
	warns []struct {
		msg  string
		args []any
	}
}

func (m *mockLogger) Warn(msg string, args ...any) {
	m.warns = append(m.warns, struct {
		msg  string
		args []any
	}{msg, args})
}

func (m *mockLogger) Error(msg string, args ...any) {}
func (m *mockLogger) Info(msg string, args ...any)  {}
func (m *mockLogger) Debug(msg string, args ...any) {}

func TestSkillInjector_Transform(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	errLogger := &mockLogger{}

	tests := []struct {
		name     string
		selector skills.SkillSelector
		req      *ports.ContextRequest
		logger   ports.Logger
		validate func(t *testing.T, req *ports.ContextRequest)
	}{
		{
			name: "InjectsSkills",
			selector: &mockSkillSelector{
				selected: []skills.Skill{
					{Name: "test-skill", Content: "Use testing."},
				},
			},
			req: &ports.ContextRequest{
				History: []*llm.Content{
					{
						Role:  "user",
						Parts: []*llm.Part{{Text: "how do I test in Go?"}},
					},
				},
			},
			validate: func(t *testing.T, req *ports.ContextRequest) {
				if len(req.History) != 2 {
					t.Fatalf("expected 2 history entries, got %d", len(req.History))
				}
				if req.History[0].Role != "system" {
					t.Errorf("expected first message to be system, got %q", req.History[0].Role)
				}
				if !req.History[0].Pinned {
					t.Error("expected injected system message to be pinned")
				}
				injectedText := req.History[0].Parts[0].Text
				if !strings.Contains(injectedText, "test-skill") {
					t.Errorf("expected injected text to contain skill name, got %q", injectedText)
				}
				if !strings.Contains(injectedText, "Use testing.") {
					t.Errorf("expected injected text to contain skill content, got %q", injectedText)
				}
			},
		},
		{
			name: "IdempotencyExistingSystem",
			selector: &mockSkillSelector{
				selected: []skills.Skill{
					{Name: "test-skill", Content: "Use testing."},
				},
			},
			req: &ports.ContextRequest{
				History: []*llm.Content{
					{
						Role: "system",
						Parts: []*llm.Part{
							{Text: "## Relevant Go Development Skills"}, // Already injected
						},
					},
				},
			},
			validate: func(t *testing.T, req *ports.ContextRequest) {
				if len(req.History[0].Parts) != 1 {
					t.Error("expected no second injection due to idempotency check")
				}
			},
		},
		{
			name:     "HandlesEmptyHistory",
			selector: &mockSkillSelector{},
			req:      &ports.ContextRequest{History: []*llm.Content{}},
			validate: func(t *testing.T, req *ports.ContextRequest) {
				// Success if it doesn't panic
			},
		},
		{
			name:     "HandlesNilSelector",
			selector: nil,
			req: &ports.ContextRequest{
				History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}},
			},
			validate: func(t *testing.T, req *ports.ContextRequest) {
				// Success if it doesn't panic
			},
		},
		{
			name: "SelectSkillsErrorIsLoggedAndSwallowed",
			selector: &mockSkillSelector{
				err: errors.New("selector unavailable"),
			},
			req: &ports.ContextRequest{
				History: []*llm.Content{
					{Role: "user", Parts: []*llm.Part{{Text: "how do I test in Go?"}}},
				},
			},
			logger: errLogger,
			validate: func(t *testing.T, req *ports.ContextRequest) {
				// Transform must NOT return an error — the failure is logged, not propagated.
				// History must remain unchanged (no injection, no mutation).
				if len(req.History) != 1 {
					t.Errorf("expected history unchanged (1 entry), got %d", len(req.History))
				}
				if req.PersistHistory {
					t.Error("expected PersistHistory to remain false when skill selection fails")
				}
				// Verify the warning was logged.
				if len(errLogger.warns) != 1 {
					t.Fatalf("expected 1 warning call, got %d", len(errLogger.warns))
				}
				if errLogger.warns[0].msg != "skill selection failed; proceeding without injected skills" {
					t.Errorf("unexpected warning message: %q", errLogger.warns[0].msg)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			injector := &skillInjector{Selector: tt.selector, Logger: tt.logger}
			err := injector.Transform(ctx, tt.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.validate(t, tt.req)
		})
	}
}

func TestSkillInjector_PriorityContract(t *testing.T) {
	t.Parallel()
	injector := &skillInjector{}

	// CRITICAL: Must execute after HistoryRepair (0) and before Gatekeeper (100)
	got := injector.Priority()
	want := 10

	if got != want {
		t.Errorf("SkillInjector.Priority() = %d; want %d. This value is part of a critical orchestration sequence contract.", got, want)
	}
}
