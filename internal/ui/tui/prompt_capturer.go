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

var _ ports.Capturer = (*promptCapturer)(nil)
var _ domain_security.UserInteractor = (*promptCapturer)(nil)

// BaseCapturer is a helper interface that joins ports.Capturer and security.UserInteractor.
type BaseCapturer interface {
	ports.Capturer
	domain_security.UserInteractor
}

// promptCapturer is an adapter that implements ports.Capturer using a Bubble Tea TUI.
type promptCapturer struct {
	base        BaseCapturer
	svc         ports.SuggestionService
	programOpts []tea.ProgramOption
}

// capturerOption defines a functional option for configuring a promptCapturer.
type capturerOption func(*promptCapturer)

// withProgramOptions allows injecting Bubble Tea program options.
func withProgramOptions(opts ...tea.ProgramOption) capturerOption {
	return func(c *promptCapturer) {
		c.programOpts = opts
	}
}

// NewPromptCapturer creates a new promptCapturer.
func NewPromptCapturer(base BaseCapturer, svc ports.SuggestionService, opts ...capturerOption) *promptCapturer {
	c := &promptCapturer{
		base: base,
		svc:  svc,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// IsTTY delegates to the base capturer.
func (c *promptCapturer) IsTTY(v any) bool {
	return c.base.IsTTY(v)
}

// CapturePrompt captures the prompt, using the TUI if requested.
func (c *promptCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...ports.CaptureOption) (string, error) {
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
	model := prompt.NewModel(c.svc, prompt.DefaultDebounceDuration)
	defer model.Destroy()

	// Initialize background logger for TUI
	if closer, err := InitLogger(); err == nil {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				log.Printf("failed to close tui logger: %v", closeErr)
			}
		}()
	}

	p := tea.NewProgram(model, c.programOpts...)

	resModel, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("tui prompt error: %w", err)
	}

	finalModel := resModel.(prompt.PromptModel)
	if finalModel.Aborted() {
		return "", context.Canceled
	}

	finalPrompt := strings.TrimSpace(finalModel.FinalPrompt())

	// Record the prompt for future suggestions
	if finalPrompt != "" {
		if err := c.svc.RecordPrompt(ctx, finalPrompt); err != nil {
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
func (c *promptCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	return c.base.Confirm(ctx, message)
}

// Warn delegates to the base capturer.
func (c *promptCapturer) Warn(message string) {
	c.base.Warn(message)
}

// Prompt delegates to the base capturer.
func (c *promptCapturer) Prompt(message string) {
	c.base.Prompt(message)
}

// ReadLine delegates to the base capturer.
func (c *promptCapturer) ReadLine(ctx context.Context) (string, error) {
	return c.base.ReadLine(ctx)
}

// ReadSingleKey delegates to the base capturer.
func (c *promptCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	return c.base.ReadSingleKey(ctx)
}

// Close closes the underlying suggestion service if it exists.
func (c *promptCapturer) Close(ctx context.Context) error {
	if c.svc != nil {
		return c.svc.Close(ctx)
	}
	return nil
}
