// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"flag"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/ui/tui/prompt"
)

var _ ports.Capturer = (*PromptCapturer)(nil)
var _ domain_security.UserInteractor = (*PromptCapturer)(nil)

// BaseCapturer is a helper interface that joins ports.Capturer and security.UserInteractor.
type BaseCapturer interface {
	ports.Capturer
	domain_security.UserInteractor
}

// PromptCapturer is an adapter that implements ports.Capturer using a Bubble Tea TUI.
type PromptCapturer struct {
	base BaseCapturer
	svc  ports.SuggestionService
}

// NewPromptCapturer creates a new PromptCapturer.
func NewPromptCapturer(base BaseCapturer, svc ports.SuggestionService) *PromptCapturer {
	return &PromptCapturer{
		base: base,
		svc:  svc,
	}
}

// IsTTY delegates to the base capturer.
func (c *PromptCapturer) IsTTY(v any) bool {
	return c.base.IsTTY(v)
}

// CapturePrompt captures the prompt, using the TUI if requested.
func (c *PromptCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...ports.CaptureOption) (string, error) {
	options := &ports.CaptureOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Fallback to base capturer if TUI is not requested or not applicable
	if !options.UseTUIPrompt || !c.IsTTY(nil) { // Simplified TTY check, base will handle it
		return c.base.CapturePrompt(ctx, fs, opts...)
	}

	// Initialize TUI Model
	// We need some default stats for the dashboard.
	// Since we are capturing the prompt BEFORE the session is fully active,
	// we use placeholders or zeroes.
	stats := prompt.SessionStats{
		TurnCount:    0,
		TokenUsage:   0,
		ProviderName: "Initializing...",
		ModelName:    "...",
	}

	model := prompt.NewModel(c.svc, stats)
	p := tea.NewProgram(model, tea.WithAltScreen())

	resModel, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("tui prompt error: %w", err)
	}

	finalModel := resModel.(*prompt.Model)
	if finalModel.Aborted() {
		return "", context.Canceled
	}

	finalPrompt := finalModel.FinalPrompt()

	// Record the prompt for future suggestions
	if finalPrompt != "" {
		_ = c.svc.RecordPrompt(finalPrompt)
	}

	return finalPrompt, nil
}

// Confirm delegates to the base capturer.
func (c *PromptCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	return c.base.Confirm(ctx, message)
}

// Warn delegates to the base capturer.
func (c *PromptCapturer) Warn(message string) {
	c.base.Warn(message)
}

// Prompt delegates to the base capturer.
func (c *PromptCapturer) Prompt(message string) {
	c.base.Prompt(message)
}

// ReadLine delegates to the base capturer.
func (c *PromptCapturer) ReadLine(ctx context.Context) (string, error) {
	return c.base.ReadLine(ctx)
}

// ReadSingleKey delegates to the base capturer.
func (c *PromptCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	return c.base.ReadSingleKey(ctx)
}
