// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"golang.org/x/term"
)

const (
	// maxPromptSize limits standard input to 1MB to prevent memory exhaustion.
	maxPromptSize = 1024 * 1024
)

// capturer handles capturing user input from TTY or pipes.
type capturer struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	SM       domain_security.ISecurityManager
	reader   *bufio.Reader
	readerMu sync.Mutex

	isTTYOverride *bool // For testing color logic
}

// NewCapturer creates a new capturer.
func NewCapturer(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.ISecurityManager) domain_security.UserInteractor {
	return &capturer{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		SM:     sm,
		reader: bufio.NewReader(stdin),
	}
}

// IsTTY returns true if the value (usually an *os.File) is a terminal.
func (c *capturer) IsTTY(v any) bool {
	if c.isTTYOverride != nil {
		return *c.isTTYOverride
	}
	if f, ok := v.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// CapturePrompt captures the initial prompt from command line arguments or standard input.
func (c *capturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts orchestration.CaptureOptions) (string, error) {
	prompt := strings.Join(fs.Args(), " ")

	if val := os.Getenv("TELL_ME_MOCK_PROMPT"); val != "" {
		return val, nil
	}

	if c.SM != nil {
		c.SM.TerminalLock()
		defer c.SM.TerminalUnlock()
	}

	var err error
	if !c.IsTTY(c.Stdin) {
		prompt, err = c.captureFromPipe(ctx, prompt)
	} else if prompt == "" && !opts.SkipTTYWait {
		prompt, err = c.captureFromTTY(ctx, !opts.Raw)
	}

	if err != nil {
		return "", err
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		if opts.SkipTTYWait {
			return "", nil
		}
		fmt.Fprintln(c.Stderr, "Usage: tell-me-go [flags] <prompt>")
		fs.PrintDefaults()
		return "", fmt.Errorf("empty prompt")
	}

	c.printFeedback(c.Stderr, !opts.Raw, colorGreen,
		fmt.Sprintf("[%s] Input captured. Processing...", time.Now().Format("15:04:05")))

	return prompt, nil
}

func (c *capturer) captureFromPipe(ctx context.Context, initialPrompt string) (string, error) {
	type result struct {
		data []byte
		err  error
	}
	readChan := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(io.LimitReader(c.Stdin, maxPromptSize))
		readChan <- result{b, err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-readChan:
		if res.err != nil {
			return "", fmt.Errorf("failed to read from pipe: %w", res.err)
		}
		if len(res.data) == 0 {
			return initialPrompt, nil
		}
		if initialPrompt != "" {
			return initialPrompt + "\n" + string(res.data), nil
		}
		return string(res.data), nil
	}
}

func (c *capturer) captureFromTTY(ctx context.Context, useColor bool) (string, error) {
	c.printFeedback(c.Stdout, useColor, colorYellow, "[Reading multi-line input. Press Ctrl+D to send]")

	type result struct {
		data []byte
		err  error
	}
	readChan := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(io.LimitReader(c.Stdin, maxPromptSize))
		readChan <- result{b, err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-readChan:
		if res.err != nil {
			return "", fmt.Errorf("failed to read from TTY: %w", res.err)
		}
		return string(res.data), nil
	}
}

// printFeedback displays a message with optional color.
// It DOES NOT perform terminal locking to avoid deadlocks when called from security components.
func (c *capturer) printFeedback(w io.Writer, useColor bool, color, msg string) {
	if useColor && c.IsTTY(w) {
		fmt.Fprintf(w, "%s%s%s\n", color, msg, colorReset)
	} else {
		fmt.Fprintln(w, msg)
	}
}

// Confirm prompts the user for confirmation.
func (c *capturer) Confirm(ctx context.Context, message string) (bool, error) {
	color := ""
	if strings.HasPrefix(message, "[SECURITY]") || strings.HasPrefix(message, "[CONFIRMATION REQUIRED]") {
		color = colorRed
	}

	if color != "" && c.IsTTY(c.Stderr) {
		fmt.Fprintf(c.Stderr, "%s%s%s", color, message, colorReset)
	} else {
		fmt.Fprint(c.Stderr, message)
	}

	char, err := c.ReadSingleKey(ctx)
	fmt.Fprintf(c.Stderr, "\n")
	if err != nil {
		return false, err
	}
	return char == "y", nil
}

// Warn displays a warning message.
func (c *capturer) Warn(message string) {
	color := colorYellow
	if strings.HasPrefix(message, "[SECURITY]") {
		color = colorRed
	}
	c.printFeedback(c.Stderr, true, color, message)
}

// Prompt displays an inline message without a newline.
func (c *capturer) Prompt(message string) {
	if c.IsTTY(c.Stderr) {
		fmt.Fprintf(c.Stderr, "%s%s%s", colorYellow, message, colorReset)
	} else {
		fmt.Fprint(c.Stderr, message)
	}
}

// ReadSingleKey waits for a single key press from Stdin.
func (c *capturer) ReadSingleKey(ctx context.Context) (string, error) {
	if val := os.Getenv("TELL_ME_MOCK_ANSWER"); val != "" {
		return strings.ToLower(val[:1]), nil
	}

	var fd int
	if f, ok := c.Stdin.(*os.File); ok {
		fd = int(f.Fd())
	} else {
		// Fallback for non-file readers in tests
		return c.readByteFallback(ctx)
	}

	if !term.IsTerminal(fd) {
		if os.Getenv("GO_WANT_HELPER_PROCESS") != "" || strings.HasSuffix(os.Args[0], ".test") {
			return c.readByteFallback(ctx)
		}
		return "", fmt.Errorf("confirmation required but not running in a terminal. Use --bypass-confirmation to skip if running in a non-interactive environment")
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		return c.readByteFallback(ctx)
	}
	defer func() { _ = term.Restore(fd, state) }()

	return c.readByteFallback(ctx)
}

func (c *capturer) readByteFallback(ctx context.Context) (string, error) {
	type result struct {
		b   byte
		err error
	}
	resChan := make(chan result, 1)

	go func() {
		c.readerMu.Lock()
		defer c.readerMu.Unlock()
		b, err := c.reader.ReadByte()
		resChan <- result{b, err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resChan:
		if res.err != nil {
			return "", res.err
		}
		if res.b == 3 { // Ctrl+C (ETX)
			return "", context.Canceled
		}
		return strings.ToLower(string(res.b)), nil
	}
}

// ReadLine reads a line of input.
func (c *capturer) ReadLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	type result struct {
		s   string
		err error
	}
	resChan := make(chan result, 1)
	go func() {
		c.readerMu.Lock()
		defer c.readerMu.Unlock()
		s, err := c.reader.ReadString('\n')
		if err != nil && (err != io.EOF || s == "") {
			resChan <- result{"", err}
		} else {
			resChan <- result{s, nil}
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resChan:
		return res.s, res.err
	}
}
