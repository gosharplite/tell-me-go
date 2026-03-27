// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

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

	// Fallback to base capturer if:
	// 1. TUI is not requested
	// 2. Not a TTY
	// 3. Positional arguments are provided (e.g. tell-me-go "hello")
	// 4. History command is provided (SkipTTYWait is true)
	if !options.UseTUIPrompt || !c.IsTTY(os.Stdin) || (fs != nil && fs.NArg() > 0) || options.SkipTTYWait {
		return c.base.CapturePrompt(ctx, fs, opts...)
	}

	// Initialize TUI Model
	model := prompt.NewModel(c.svc)

	// Initialize background logger for TUI
	if closer, err := InitLogger(); err == nil {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				log.Printf("failed to close tui logger: %v", closeErr)
			}
		}()
	}

	p := tea.NewProgram(model)

	resModel, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("tui prompt error: %w", err)
	}

	finalModel := resModel.(*prompt.Model)
	if finalModel.Aborted() {
		return "", context.Canceled
	}

	finalPrompt := strings.TrimSpace(finalModel.FinalPrompt())

	// Record the prompt for future suggestions
	if finalPrompt != "" {
		if err := c.svc.RecordPrompt(finalPrompt); err != nil {
			log.Printf("failed to record prompt for suggestions: %v", err)
		}

		// Provide visual feedback after the TUI closes so the user knows what was sent.
		// This uses the base capturer's Prompt method to stay consistent with the tool's theme.
		timestamp := time.Now().Format("15:04:05")
		c.base.Prompt(fmt.Sprintf("[%s] Input captured:\n", timestamp))
		fmt.Fprintln(os.Stderr, finalPrompt)
		c.base.Prompt(fmt.Sprintf("[%s] Processing...\n", timestamp))
	} else if !options.SkipTTYWait {
		// Return an error for empty prompt to match the base capturer's behavior
		return "", fmt.Errorf("empty prompt")
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
