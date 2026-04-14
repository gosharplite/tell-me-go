// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
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
func (c *promptCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	options := &ports.CaptureOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if c.shouldFallback(args, options) {
		return c.base.CapturePrompt(ctx, args, opts...)
	}

	finalPrompt, err := c.runTUI(ctx)
	if err != nil {
		return "", err
	}

	if finalPrompt == "" && !options.SkipTTYWait {
		return "", context.Canceled
	}

	c.provideFeedback(ctx, finalPrompt)
	return finalPrompt, nil
}

// shouldFallback encapsulates the routing logic that determines if the TUI should be skipped.
func (c *promptCapturer) shouldFallback(args []string, options *ports.CaptureOptions) bool {
	return !options.UseTUIPrompt || !c.IsTTY(os.Stdin) || len(args) > 0 || options.SkipTTYWait
}

// runTUI moves the Bubble Tea initialization, logging, and execution into a dedicated method.
func (c *promptCapturer) runTUI(ctx context.Context) (string, error) {
	model := prompt.NewModel(c.svc, prompt.DefaultDebounceDuration)
	defer model.Destroy()

	// Handle TUI logger lifecycle
	if closer, err := InitLogger(); err == nil {
		defer func() { _ = closer.Close() }()
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

	return strings.TrimSpace(finalModel.FinalPrompt()), nil
}

// provideFeedback moves the logic for recording the prompt and displaying the "Input captured" footer into a helper.
func (c *promptCapturer) provideFeedback(ctx context.Context, finalPrompt string) {
	if finalPrompt == "" {
		return
	}

	if err := c.svc.RecordPrompt(ctx, finalPrompt); err != nil {
		log.Printf("failed to record prompt for suggestions: %v", err)
	}

	timestamp := time.Now().Format("15:04:05")
	c.base.Prompt(fmt.Sprintf("[%s] Input captured:\n", timestamp))
	fmt.Fprintln(os.Stderr, finalPrompt)
	c.base.Prompt(fmt.Sprintf("[%s] Processing...\n", timestamp))
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

// Close closes the underlying suggestion service and the base capturer.
func (c *promptCapturer) Close(ctx context.Context) error {
	var errs []error
	if c.svc != nil {
		if err := c.svc.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if c.base != nil {
		if err := c.base.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close error: %v", errs)
	}
	return nil
}
