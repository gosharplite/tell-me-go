// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/pkg/idgen"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/gosharplite/tell-me-go/internal/ui/tui"
	"github.com/gosharplite/tell-me-go/internal/ui/tui/progress"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// chatCommand implements the main chat command.
type chatCommand struct {
	Version          string
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	SM               domain_security.Manager
	ChatService      ports.ChatService
	Bootstrapper     Bootstrapper
	Loader           domain_config.ConfigLoader
	HomeDir          string
	MockPrompt       string
	MockAnswer       string
	Interactor       *InteractorRef
	CallbackNotifier domain_callback.CallbackNotifier
	ModeLocker       domain_persistence.ModeLocker
	cmd              *cobra.Command
	capturerOverride ports.CapturerInteractor                                                                                                                                                                // test-only injection
	capturerFactory  func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor // test-only injection; defaults to ui.NewCapturer
}

// newCapturer calls the injected factory or falls back to ui.NewCapturer.
func (c *chatCommand) newCapturer(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
	if c.capturerFactory != nil {
		return c.capturerFactory(stdin, stdout, stderr, sm, clk, mockPrompt, mockAnswer, disableEscapeSequences)
	}
	return ui.NewCapturer(stdin, stdout, stderr, sm, clk, mockPrompt, mockAnswer, disableEscapeSequences)
}

// warnf writes a formatted warning message to stderr; errors from Fprintf are
// deliberately discarded because there is no recovery path for logging failures.
func (c *chatCommand) warnf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(c.Stderr, format, args...)
}

type cliOptions struct {
	configPath      string
	newSession      bool
	showTurnsLog    bool
	diagnostic      bool
	jsonOutput      bool
	lastN           int
	backN           int
	rawOutput       bool
	tuiPrompt       bool
	tuiOutput       bool
	retry           bool
	editLast        bool
	updateTurnText  string
	callbackURL     string
	callbackID      string
	callbackHeaders []string
}

func addChatFlags(fs *pflag.FlagSet, opts *cliOptions) {
	fs.BoolVar(&opts.newSession, "new", false, "Start a new session")
	fs.BoolVarP(&opts.showTurnsLog, "turns", "t", false, "Print the contents of the current session's turns.log and exit")
	fs.BoolVarP(&opts.diagnostic, "diagnostic", "d", false, "Run a comprehensive system health check and exit")
	fs.BoolVar(&opts.jsonOutput, "json", false, "Output in JSON format (for diagnostics)")
	fs.IntVarP(&opts.lastN, "last", "l", 0, "Show the last N messages from history")
	fs.Lookup("last").NoOptDefVal = "1"
	fs.IntVarP(&opts.backN, "back", "b", 0, "Go back / delete the last N turns from history")
	fs.Lookup("back").NoOptDefVal = "1"
	fs.BoolVarP(&opts.rawOutput, "raw", "r", false, "Show raw output (without markdown rendering)")
	fs.BoolVarP(&opts.tuiPrompt, "interactive", "i", false, "Enable interactive TUI prompt with suggestions")
	fs.BoolVarP(&opts.tuiOutput, "tui-output", "o", false, "Enable TUI progress dashboard during agent turns")
	fs.BoolVar(&opts.retry, "retry", false, "Retry the last user message")
	fs.BoolVarP(&opts.editLast, "edit-last", "e", false, "Edit the last model response (text and thinking) in an interactive TUI")
	fs.StringVar(&opts.updateTurnText, "update-turn", "__NOT_SET__", "Replace text of the last model response headlessly; use empty string \"\" to delete the turn instead (for inter-agent refusal recovery)")
	fs.StringVar(&opts.callbackURL, "callback", "", "Webhook URL to notify upon completion (asynchronous worker mode)")
	fs.StringVar(&opts.callbackID, "callback-id", "", "Custom correlation ID for callback worker session")
	fs.StringArrayVar(&opts.callbackHeaders, "callback-header", nil, "Custom HTTP header for webhook callback (format: 'Name: Value', repeatable)")
}

// newChatCommand creates a new Chat Command as a Cobra command.
func newChatCommand(ctx *context, opts *cliOptions) *cobra.Command {
	c := &chatCommand{
		Version:          ctx.Version,
		Stdin:            ctx.Stdin,
		Stdout:           ctx.Stdout,
		Stderr:           ctx.Stderr,
		SM:               ctx.SM,
		ChatService:      ctx.ChatService,
		Bootstrapper:     ctx.Bootstrapper,
		Loader:           ctx.Loader,
		HomeDir:          ctx.HomeDir,
		MockPrompt:       ctx.MockPrompt,
		MockAnswer:       ctx.MockAnswer,
		Interactor:       ctx.Interactor,
		CallbackNotifier: ctx.CallbackNotifier,
		ModeLocker:       ctx.ModeLocker,
	}
	c.capturerFactory = ui.NewCapturer

	if opts == nil {
		opts = &cliOptions{updateTurnText: "__NOT_SET__"}
	}

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Start a chat session (Default)",
		Long:  `The chat command initiates a session with the AI assistant. You can provide a prompt directly as an argument or enter an interactive session.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c.cmd = cmd
			configPath, _ := cmd.Flags().GetString("config") // flag guaranteed by root command; never errors
			opts.configPath = configPath
			return c.executeChat(cmd.Context(), opts, args)
		},
	}
	c.cmd = cmd

	addChatFlags(cmd.Flags(), opts)

	return cmd
}

// executeChat runs the chat command logic.
func (c *chatCommand) executeChat(ctx stdctx.Context, opts *cliOptions, args []string) error {
	cfg, err := c.Loader.Load(opts.configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	if c.isCallbackRequested(opts) {
		return c.handleCallbackPreflight(ctx, cfg, opts, args)
	}

	if handled, err := c.handleEarlyWorkflow(ctx, cfg, opts); handled {
		return err
	}

	return c.runChatSession(ctx, cfg, opts, args)
}

func (c *chatCommand) isCallbackRequested(opts *cliOptions) bool {
	if opts.callbackURL != "" {
		return true
	}
	return c.cmd != nil && c.cmd.Flags().Changed("callback")
}

func (c *chatCommand) handleCallbackPreflight(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions, args []string) error {
	preflight, err := c.validateCallbackPreflight(ctx, cfg, opts, args)
	if err != nil {
		return err
	}
	if preflight != nil && preflight.releaseLock != nil {
		defer preflight.releaseLock()
	}
	return nil
}

func (c *chatCommand) handleEarlyWorkflow(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) (bool, error) {
	if opts.diagnostic {
		return true, c.handleDiagnosticWorkflow(ctx, cfg, opts)
	}
	if opts.showTurnsLog {
		return true, c.handleTurnsLogWorkflow(ctx, cfg, opts)
	}
	if opts.editLast {
		return true, c.handleEditLastWorkflow(ctx, cfg, opts)
	}
	if opts.updateTurnText != "__NOT_SET__" {
		return true, c.handleUpdateTurnWorkflow(ctx, cfg, opts)
	}
	return false, nil
}

func (c *chatCommand) runChatSession(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions, args []string) error {
	capturer, cleanup, err := c.setupChatSession(ctx, cfg, opts, args)
	if err != nil {
		return err
	}
	defer func() {
		timeout := ports.DefaultShutdownTimeout
		if !opts.tuiPrompt {
			timeout = 100 * time.Millisecond
		}
		shutdownCtx, cancel := stdctx.WithTimeout(stdctx.Background(), timeout)
		defer cancel()
		_ = cleanup(shutdownCtx)
	}()

	return c.processChatRequest(ctx, cfg, opts, args, capturer)
}

var (
	rfc7230TokenRegex    = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~a-zA-Z0-9]+$")
	callbackIDRegex      = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	sensitiveHeaderRegex = regexp.MustCompile(`(?i)(auth|token|key|secret|cookie|pass|cred)`)
)

type callbackPreflightResult struct {
	sessionID   string
	prompt      string
	headers     map[string]string
	releaseLock func()
}

// maskSensitiveHeader masks header values in stderr telemetry.
// If the header name matches case-insensitive auth|token|key|secret|cookie|pass|cred,
// the value is masked as "***" (or "Bearer ***" if prefixed with "Bearer ").
func maskSensitiveHeader(name, val string) string {
	if sensitiveHeaderRegex.MatchString(name) {
		if strings.HasPrefix(strings.ToLower(val), "bearer ") {
			return "Bearer ***"
		}
		return "***"
	}
	return val
}

func validateBypassGuard(c *chatCommand, cfg *domain_config.Config) error {
	if cfg == nil || !cfg.BypassConfirmation {
		c.warnf("error: callback worker mode requires BYPASS_CONFIRMATION: true in config\n")
		return errors.New("callback worker mode requires BYPASS_CONFIRMATION: true in config")
	}
	return nil
}

func validateWhitelistGuard(c *chatCommand) error {
	if c.cmd == nil {
		return nil
	}
	allowedFlags := map[string]bool{
		"config":          true,
		"new":             true,
		"callback":        true,
		"callback-id":     true,
		"callback-header": true,
	}
	var disallowedFlag string
	c.cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if disallowedFlag != "" {
			return
		}
		if f.Changed && !allowedFlags[f.Name] {
			disallowedFlag = f.Name
		}
	})
	if disallowedFlag != "" {
		c.warnf("error: flag --%s is not allowed in callback mode\n", disallowedFlag)
		return fmt.Errorf("flag --%s is not allowed in callback mode", disallowedFlag)
	}
	return nil
}

func validateCallbackURL(c *chatCommand, rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		c.warnf("error: invalid callback URL %q: must have http or https scheme with non-empty host\n", rawURL)
		return fmt.Errorf("invalid callback URL %q: must have http or https scheme with non-empty host", rawURL)
	}
	return nil
}

func parseAndValidateHeader(c *chatCommand, h string) (string, string, error) {
	idx := strings.Index(h, ":")
	if idx == -1 {
		c.warnf("error: invalid callback header format %q: expected 'Name: Value'\n", h)
		return "", "", fmt.Errorf("invalid callback header format %q: expected 'Name: Value'", h)
	}
	name := h[:idx]
	rawVal := h[idx+1:]
	if !rfc7230TokenRegex.MatchString(name) {
		c.warnf("error: invalid callback header name %q: must match RFC 7230 token format (value: %s)\n", name, maskSensitiveHeader(name, strings.TrimSpace(rawVal)))
		return "", "", fmt.Errorf("invalid callback header name %q: must match RFC 7230 token format", name)
	}
	if strings.ContainsAny(rawVal, "\r\n") {
		c.warnf("error: callback header %q value contains illegal CR or LF characters (value: %s)\n", name, maskSensitiveHeader(name, strings.TrimSpace(rawVal)))
		return "", "", fmt.Errorf("callback header %q value contains illegal CR or LF characters", name)
	}
	return name, strings.TrimSpace(rawVal), nil
}

func validateCallbackHeaders(c *chatCommand, rawHeaders []string) (map[string]string, error) {
	if len(rawHeaders) == 0 {
		return nil, nil
	}
	headers := make(map[string]string, len(rawHeaders))
	for _, h := range rawHeaders {
		name, val, err := parseAndValidateHeader(c, h)
		if err != nil {
			return nil, err
		}
		headers[name] = val
	}
	return headers, nil
}

func resolveAndValidateCallbackID(c *chatCommand, rawID string) (string, error) {
	idChanged := c.cmd != nil && c.cmd.Flags().Changed("callback-id")
	if idChanged {
		if rawID == "" {
			c.warnf("error: --callback-id cannot be empty\n")
			return "", errors.New("--callback-id cannot be empty")
		}
		if !callbackIDRegex.MatchString(rawID) {
			c.warnf("error: invalid --callback-id %q: must match ^[A-Za-z0-9._:-]{1,128}$\n", rawID)
			return "", fmt.Errorf("invalid --callback-id %q: must match ^[A-Za-z0-9._:-]{1,128}$", rawID)
		}
		return rawID, nil
	}
	if rawID != "" {
		if !callbackIDRegex.MatchString(rawID) {
			c.warnf("error: invalid --callback-id %q: must match ^[A-Za-z0-9._:-]{1,128}$\n", rawID)
			return "", fmt.Errorf("invalid --callback-id %q: must match ^[A-Za-z0-9._:-]{1,128}$", rawID)
		}
		return rawID, nil
	}
	return idgen.Generate(), nil
}

func acquireModeLock(c *chatCommand, cfg *domain_config.Config) (func(), error) {
	if c.ModeLocker == nil {
		c.warnf("error: mode locker is not configured\n")
		return nil, errors.New("mode locker is not configured")
	}
	mode := ""
	if cfg != nil {
		mode = cfg.Mode
	}
	releaseLock, err := c.ModeLocker.TryLockMode(mode)
	if err != nil {
		c.warnf("error: failed to acquire mode lock for %q: %v\n", mode, err)
		return nil, fmt.Errorf("failed to acquire mode lock for %q: %w", mode, err)
	}
	return releaseLock, nil
}

func captureCallbackPrompt(c *chatCommand, args []string) (string, error) {
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" && c.Stdin != nil {
		stdinBytes, err := io.ReadAll(c.Stdin)
		if err != nil {
			c.warnf("error: failed to read prompt from stdin: %v\n", err)
			return "", fmt.Errorf("failed to read prompt from stdin: %w", err)
		}
		prompt = strings.TrimSpace(string(stdinBytes))
	}
	if prompt == "" {
		c.warnf("error: prompt cannot be empty in callback mode\n")
		return "", errors.New("prompt cannot be empty in callback mode")
	}
	return prompt, nil
}

func (c *chatCommand) validateCallbackPreflight(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions, args []string) (*callbackPreflightResult, error) {
	// Step 1: D2 Guard: BYPASS_CONFIRMATION: true required in config
	if err := validateBypassGuard(c, cfg); err != nil {
		return nil, err
	}

	// Step 2: A4 Whitelist Guard: every Changed() flag must be in {"config", "new", "callback", "callback-id", "callback-header"}
	if err := validateWhitelistGuard(c); err != nil {
		return nil, err
	}

	// Step 3: URL Scheme: URL must be parsed and have scheme http or https with non-empty host
	if err := validateCallbackURL(c, opts.callbackURL); err != nil {
		return nil, err
	}

	// Step 4: Header Validation: Each header must be 'Name: Value'.
	// Name must match RFC 7230 token ^[!#$%&'*+\-.^_`|~a-zA-Z0-9]+$. Value rejects \r and \n.
	// Mask sensitive values in stderr telemetry.
	headers, err := validateCallbackHeaders(c, opts.callbackHeaders)
	if err != nil {
		return nil, err
	}

	// Step 5: ID Charset / Resolution: If --callback-id changed, reject empty or values not matching ^[A-Za-z0-9._:-]{1,128}$.
	// If unchanged, generate via idgen.Generate().
	sessionID, err := resolveAndValidateCallbackID(c, opts.callbackID)
	if err != nil {
		return nil, err
	}

	// Step 6: Mode Lock: Acquire lock via c.ModeLocker.TryLockMode(cfg.Mode). If contended, fail fast.
	releaseLock, err := acquireModeLock(c, cfg)
	if err != nil {
		return nil, err
	}

	// Step 7: Prompt Check: Capture prompt from CLI args (joined) or c.Stdin. If empty, fail fast.
	prompt, err := captureCallbackPrompt(c, args)
	if err != nil {
		if releaseLock != nil {
			releaseLock()
		}
		return nil, err
	}

	return &callbackPreflightResult{
		sessionID:   sessionID,
		prompt:      prompt,
		headers:     headers,
		releaseLock: releaseLock,
	}, nil
}

func (c *chatCommand) handleDiagnosticWorkflow(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) error {
	return c.ChatService.RunDiagnostics(ctx, cfg, opts.configPath, opts.jsonOutput)
}

func (c *chatCommand) handleTurnsLogWorkflow(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) error {
	return c.ChatService.StreamTurnsLog(ctx, cfg, c.Stdout)
}

func (c *chatCommand) handleEditLastWorkflow(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) error {
	hManager, err := c.Bootstrapper.GetHistoryManager(ctx, cfg)
	if err != nil {
		return fmt.Errorf("error getting history manager for edit: %w", err)
	}
	err = c.ChatService.EditLastTurn(ctx, hManager)
	if errors.Is(err, ports.ErrEditAborted) {
		return nil // user aborted, not an error
	}
	return err
}

func (c *chatCommand) handleUpdateTurnWorkflow(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) error {
	hManager, err := c.Bootstrapper.GetHistoryManager(ctx, cfg)
	if err != nil {
		return fmt.Errorf("error getting history manager for update-turn: %w", err)
	}
	return c.ChatService.UpdateLastTurn(ctx, hManager, opts.updateTurnText)
}

func (c *chatCommand) isTUIConfigured(cfg *domain_config.Config) bool {
	return cfg != nil && cfg.UseTUIPrompt
}

func (c *chatCommand) noOtherActionsRequested(opts *cliOptions, args []string) bool {
	return len(args) == 0 && opts.lastN == 0 && opts.backN == 0 && !opts.retry
}

func (c *chatCommand) setupChatSession(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions, args []string) (ports.CapturerInteractor, func(stdctx.Context) error, error) {
	// Apply TUI overrides and state detection
	if opts.tuiPrompt {
		cfg.UseTUIPrompt = true
	}
	// Only auto-enable TUI from config if no other actions are requested
	if c.isTUIConfigured(cfg) && c.noOtherActionsRequested(opts, args) {
		opts.tuiPrompt = true
	}
	return c.getCapturer(ctx, cfg, opts)
}

// getCapturer returns the override if set (test path), otherwise delegates
// to buildCapturer for the production path.
func (c *chatCommand) getCapturer(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) (ports.CapturerInteractor, func(stdctx.Context) error, error) {
	if c.capturerOverride != nil {
		return c.capturerOverride, func(shutdownCtx stdctx.Context) error {
			if err := c.capturerOverride.Close(shutdownCtx); err != nil {
				c.warnf("Warning: failed to close capturer: %v\n", err)
				return err
			}
			return nil
		}, nil
	}
	return c.buildCapturer(ctx, cfg, opts)
}
func (c *chatCommand) processChatRequest(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions, args []string, capturer ports.CapturerInteractor) error {
	prompt, err := c.captureInput(ctx, capturer, opts, args)
	if err != nil {
		return err
	}
	return c.ChatService.ProcessMessage(ctx, cfg, ports.ChatCommand{
		ConfigPath:       opts.configPath,
		NewSession:       opts.newSession,
		LastN:            opts.lastN,
		BackN:            opts.backN,
		RawOutput:        opts.rawOutput,
		UseTUIPrompt:     opts.tuiPrompt,
		TUIOutput:        opts.tuiOutput,
		ProgressRenderer: progress.NewRenderer(c.Bootstrapper.GetSystemMetricsProvider()),
		Retry:            opts.retry,
		Prompt:           prompt,
	}, capturer)
}
func (c *chatCommand) captureInput(ctx stdctx.Context, capturer ports.CapturerInteractor, opts *cliOptions, args []string) (string, error) {
	if opts.retry {
		return "", nil
	}

	captureOpts := c.prepareCaptureOptions(opts)
	prompt, err := capturer.CapturePrompt(ctx, args, captureOpts...)
	if err != nil {
		if !errors.Is(err, ui.ErrNoInput) {
			return "", err
		}
		// Continue with empty prompt if we were told to skip TTY wait (e.g. -l or -b was used)
	}

	return prompt, nil
}

// buildTUICapturer constructs a TUI-based prompt capturer with suggestion support.
// It seeds the suggestion trie from the last user message (best-effort), initializes
// the suggestion service, and wraps the base capturer. Falls back to a bare capturer
// if suggestion initialization fails.
func (c *chatCommand) buildTUICapturer(ctx stdctx.Context, cfg *domain_config.Config) (ports.CapturerInteractor, func(stdctx.Context) error, error) {
	// Try to get at least the last user message for the trie
	hManager, _ := c.Bootstrapper.GetHistoryManager(ctx, cfg)
	var lastMsg string
	if hManager != nil {
		lastMsg, _, _ = c.ChatService.GetLastUserMessage(ctx, hManager)
	}

	var recentHistory []string
	if lastMsg != "" {
		recentHistory = append(recentHistory, lastMsg)
	}

	svc, err := c.Bootstrapper.GetSuggestionService(ctx, recentHistory)

	capturerInterface := c.newCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM, clock.RealClock{}, c.MockPrompt, c.MockAnswer, false)
	baseCapturer, ok := capturerInterface.(tui.BaseCapturer)
	if !ok {
		// Coverage gap accepted by architect: structurally unreachable.
		// tui.BaseCapturer and ports.CapturerInteractor are structurally
		// identical interfaces (both compose ports.Capturer +
		// domain_security.UserInteractor). Any type that fails the
		// BaseCapturer assertion necessarily also fails the
		// CapturerInteractor assertion — and vice versa. The dual-failure
		// path is covered by the error return below. See Issue #888.
		return nil, nil, fmt.Errorf("ui.NewCapturer did not return a tui.BaseCapturer or ports.CapturerInteractor")
	}

	var capturer ports.CapturerInteractor
	var cleanup func(stdctx.Context) error

	if err != nil {
		// Log warning and fall back to the base capturer (no suggestions)
		c.warnf("Warning: failed to initialize suggestions: %v\n", err)
		capturer = baseCapturer
		cleanup = func(ctx stdctx.Context) error {
			return capturer.Close(ctx)
		}
	} else {
		capturer = tui.NewPromptCapturer(baseCapturer, svc)
		cleanup = func(ctx stdctx.Context) error {
			if err := capturer.Close(ctx); err != nil {
				c.warnf("Warning: failed to close capturer: %v\n", err)
				return err
			}
			return nil
		}
	}

	c.Interactor.set(capturer)
	return capturer, cleanup, nil
}

func (c *chatCommand) buildCapturer(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) (ports.CapturerInteractor, func(stdctx.Context) error, error) {
	if opts.tuiPrompt {
		return c.buildTUICapturer(ctx, cfg)
	}
	capturer, cleanup, err := c.setupCapturer()
	if err != nil {
		return nil, nil, err
	}
	return capturer, cleanup, nil
}

func (c *chatCommand) setupCapturer() (ports.CapturerInteractor, func(stdctx.Context) error, error) {
	if c.capturerOverride != nil {
		return c.capturerOverride, func(ctx stdctx.Context) error {
			if err := c.capturerOverride.Close(ctx); err != nil {
				c.warnf("Warning: failed to close capturer: %v\n", err)
				return err
			}
			return nil
		}, nil
	}

	capturerInterface := c.newCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM, clock.RealClock{}, c.MockPrompt, c.MockAnswer, false)
	capturer, ok := capturerInterface.(ports.CapturerInteractor)
	if !ok {
		return nil, nil, fmt.Errorf("ui.NewCapturer did not return an ports.CapturerInteractor")
	}
	c.Interactor.set(capturer)
	return capturer, func(ctx stdctx.Context) error {
		if err := capturer.Close(ctx); err != nil {
			c.warnf("Warning: failed to close capturer: %v\n", err)
			return err
		}
		return nil
	}, nil
}

func (c *chatCommand) prepareCaptureOptions(opts *cliOptions) []ports.CaptureOption {
	var captureOpts []ports.CaptureOption
	if opts.lastN > 0 || opts.backN > 0 {
		captureOpts = append(captureOpts, ports.WithSkipTTYWait(true))
	}
	if opts.rawOutput {
		captureOpts = append(captureOpts, ports.WithRaw(true))
	}
	if opts.tuiPrompt {
		captureOpts = append(captureOpts, ports.WithTUIPrompt(true))
	}
	return captureOpts
}
