// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSkillSelector struct {
	selected []skills.Skill
	err      error
}

func (m *mockSkillSelector) SelectSkills(ctx context.Context, taskDescription string) ([]skills.Skill, error) {
	return m.selected, m.err
}

func TestSkillInjector_Transform(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// errLogger is intentionally shared via closure capture by exactly ONE test case
	// ("SelectSkillsErrorIsLoggedAndSwallowed"). No other case sets logger to a non-nil
	// value, so no data race exists. SpyLogger is goroutine-safe, so accidental reuse
	// is safe.
	errLogger := &testfixtures.SpyLogger{}

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
				require.Len(t, req.History, 2)
				assert.Equal(t, "system", req.History[0].Role)
				assert.True(t, req.History[0].Pinned, "expected injected system message to be pinned")
				injectedText := req.History[0].Parts[0].Text
				assert.Contains(t, injectedText, "test-skill")
				assert.Contains(t, injectedText, "Use testing.")
			},
		},
		{
			name: "InjectsSkillsIntoExistingSystem",
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
							{Text: "You are a helpful assistant."},
						},
					},
				},
			},
			validate: func(t *testing.T, req *ports.ContextRequest) {
				require.Len(t, req.History, 1, "should not prepend new message")
				require.Len(t, req.History[0].Parts, 2, "should append injection part")
				assert.Equal(t, "You are a helpful assistant.", req.History[0].Parts[0].Text)
				assert.Contains(t, req.History[0].Parts[1].Text, "test-skill")
				assert.Contains(t, req.History[0].Parts[1].Text, "Use testing.")
				assert.True(t, req.History[0].Pinned, "system message must be pinned after injection")
				assert.True(t, req.PersistHistory, "PersistHistory must be set after injection")
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
			name: "SelectSkillsReturnsEmpty",
			selector: &mockSkillSelector{
				selected: []skills.Skill{},
			},
			req: &ports.ContextRequest{
				History: []*llm.Content{
					{Role: "user", Parts: []*llm.Part{{Text: "some task"}}},
				},
			},
			validate: func(t *testing.T, req *ports.ContextRequest) {
				assert.Len(t, req.History, 1, "history should be unchanged")
				assert.False(t, req.PersistHistory, "PersistHistory should not be set")
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
				assert.Len(t, req.History, 1, "expected history unchanged")
				assert.False(t, req.PersistHistory, "expected PersistHistory to remain false when skill selection fails")
				// Verify the warning was logged.
				warns := errLogger.GetWarns()
				require.Len(t, warns, 1)
				assert.Equal(t, "skill selection failed; proceeding without injected skills", warns[0])
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

func TestSkillInjector_IsAlreadyInjected(t *testing.T) {
	t.Parallel()
	injector := &skillInjector{}

	tests := []struct {
		name    string
		content *llm.Content
		want    bool
	}{
		{
			name: "ReturnsFalseWhenMarkerAbsent",
			content: &llm.Content{
				Role: "system",
				Parts: []*llm.Part{
					{Text: "You are a helpful assistant."},
				},
			},
			want: false,
		},
		{
			name: "ReturnsFalseWhenPartsEmpty",
			content: &llm.Content{
				Role:  "system",
				Parts: []*llm.Part{},
			},
			want: false,
		},
		{
			name: "ReturnsTrueWhenMarkerPresent",
			content: &llm.Content{
				Role: "system",
				Parts: []*llm.Part{
					{Text: "## Relevant Go Development Skills\n\n..."},
				},
			},
			want: true,
		},
		{
			name: "ReturnsTrueWhenMarkerInLaterPart",
			content: &llm.Content{
				Role: "system",
				Parts: []*llm.Part{
					{Text: "You are a helpful assistant."},
					{Text: "## Relevant Go Development Skills\n\n..."},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := injector.isAlreadyInjected(tt.content)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewSkillInjector(t *testing.T) {
	t.Parallel()
	selector := &mockSkillSelector{}
	logger := &testfixtures.SpyLogger{}

	transformer := NewSkillInjector(selector, logger)

	require.NotNil(t, transformer, "NewSkillInjector must return a non-nil transformer")

	// Interface satisfaction is verified at compile time by the return type
	// (NewSkillInjector returns ports.ContextTransformer).

	// Verify Priority contract through the interface
	assert.Equal(t, 10, transformer.Priority())

	// Verify basic usability (no panic on empty history)
	req := &ports.ContextRequest{History: []*llm.Content{}}
	err := transformer.Transform(context.Background(), req)
	require.NoError(t, err)
}

func TestSkillInjector_EcosystemIntro_OptionAndConstructor(t *testing.T) {
	t.Parallel()

	t.Run("WithSkillEcosystemIntro sets field", func(t *testing.T) {
		t.Parallel()
		si := &skillInjector{}
		opt := WithSkillEcosystemIntro("skills.sh ecosystem available")
		opt(si)
		assert.Equal(t, "skills.sh ecosystem available", si.ecosystemIntro)
	})

	t.Run("NewSkillInjector applies ecosystem intro option", func(t *testing.T) {
		t.Parallel()
		selector := &mockSkillSelector{selected: []skills.Skill{}}
		logger := &testfixtures.SpyLogger{}

		transformer := NewSkillInjector(selector, logger,
			WithSkillEcosystemIntro("skills.sh ecosystem available"))
		require.NotNil(t, transformer)

		// Verify via Transform: ecosystem intro is injected even when no skills match
		req := &ports.ContextRequest{
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "some task"}}},
			},
		}
		err := transformer.Transform(context.Background(), req)
		require.NoError(t, err)

		// Ecosystem intro should be injected as a new system message
		require.Len(t, req.History, 2, "expected new system message prepended")
		assert.Equal(t, "system", req.History[0].Role)
		assert.Contains(t, req.History[0].Parts[0].Text, "skills.sh ecosystem available")

		// CRITICAL: PersistHistory must be false — ecosystem intro does NOT trigger
		// history persistence (only actual skill content triggers it).
		assert.False(t, req.PersistHistory,
			"ecosystem intro alone must not trigger PersistHistory")
	})
}

func TestSkillInjector_EcosystemIntro_BuildAndIdempotency(t *testing.T) {
	t.Parallel()

	t.Run("buildInjectionBlock appends ecosystem intro after skills", func(t *testing.T) {
		t.Parallel()
		si := &skillInjector{ecosystemIntro: "Available: k8s, ado, atlassian"}

		selected := []skills.Skill{
			{Name: "k8s-patterns", Content: "Use kubectl apply."},
		}
		block := si.buildInjectionBlock(selected)

		// Verify skill content is present
		assert.Contains(t, block, "k8s-patterns")
		assert.Contains(t, block, "Use kubectl apply.")

		// Verify ecosystem intro is appended AFTER skill content
		assert.Contains(t, block, "Available: k8s, ado, atlassian")

		// Verify ecosystem intro comes AFTER the skill separator and BEFORE end of block.
		// The intro must not appear inside a skill's content section.
		skillEnd := "Use kubectl apply."
		ecosystemStart := "Available: k8s, ado, atlassian"
		assert.True(t,
			strings.Index(block, skillEnd) < strings.Index(block, ecosystemStart),
			"ecosystem intro must appear after skill content, not before or inside it")
	})

	t.Run("buildInjectionBlock without ecosystem intro omits it", func(t *testing.T) {
		t.Parallel()
		si := &skillInjector{} // ecosystemIntro is empty

		selected := []skills.Skill{
			{Name: "k8s-patterns", Content: "Use kubectl apply."},
		}
		block := si.buildInjectionBlock(selected)

		assert.Contains(t, block, "k8s-patterns")
		assert.NotContains(t, block, "Available:", "empty ecosystem intro must not inject placeholder text")
	})

	t.Run("isAlreadyInjected detects ecosystem intro text", func(t *testing.T) {
		t.Parallel()
		si := &skillInjector{ecosystemIntro: "Available: k8s, ado, atlassian"}

		content := &llm.Content{
			Role: "system",
			Parts: []*llm.Part{
				{Text: "You are a helpful assistant.\n\nAvailable: k8s, ado, atlassian\n"},
			},
		}
		assert.True(t, si.isAlreadyInjected(content),
			"must detect ecosystem intro as already injected")
	})

	t.Run("isAlreadyInjected returns false when intro absent", func(t *testing.T) {
		t.Parallel()
		si := &skillInjector{ecosystemIntro: "Available: k8s, ado, atlassian"}

		content := &llm.Content{
			Role:  "system",
			Parts: []*llm.Part{{Text: "You are a helpful assistant."}},
		}
		assert.False(t, si.isAlreadyInjected(content))
	})
}
