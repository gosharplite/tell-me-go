// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"golang.org/x/term"
)

var (
	// ErrNoInput is returned when no input is provided and SkipTTYWait is true.
	ErrNoInput = errors.New("no input")
	// errCapturerClosed is returned when an operation is attempted on a closed capturer.
	errCapturerClosed = errors.New("capturer closed")
)

const (
	// maxPromptSize limits standard input to 1MB to prevent memory exhaustion.
	maxPromptSize = 1024 * 1024
)

type readOp int

const (
	opReadByte readOp = iota
	opReadString
	opReadAll
)

type readRequest struct {
	op    readOp
	delim byte  // For ReadString
	limit int64 // For ReadAll (use maxPromptSize)
	resCh chan ioResult
	ctx   context.Context // Context to allow cancellation
}

type ioResult struct {
	data any // Will hold byte, string, or []byte
	err  error
}

// capturer handles capturing user input from TTY or pipes.
type capturer struct {
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	SM          domain_security.Manager
	Clock       clock.Clock
	reader      *bufio.Reader
	requestChan chan readRequest
	done        chan struct{}
	readerMu    sync.Mutex // Protects requestChan state during shutdown
	mockPrompt  string
	mockAnswer  string

	isTTYOverride          *bool // For testing color logic
	disableEscapeSequences bool
}

// NewCapturer creates a new capturer.
func NewCapturer(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
	if clk == nil {
		clk = clock.RealClock{}
	}
	c := &capturer{
		Stdin:                  stdin,
		Stdout:                 stdout,
		Stderr:                 stderr,
		SM:                     sm,
		Clock:                  clk,
		reader:                 bufio.NewReader(stdin),
		requestChan:            make(chan readRequest),
		done:                   make(chan struct{}),
		mockPrompt:             mockPrompt,
		mockAnswer:             mockAnswer,
		disableEscapeSequences: disableEscapeSequences,
	}
	c.startWorker()
	return c
}

func (c *capturer) startWorker() {
	reqChan := c.requestChan
	go func() {
		defer close(c.done)
		for req := range reqChan {
			var res ioResult

			// Race context against the blocking read operation.
			readDone := make(chan ioResult, 1)
			go func(req readRequest) {
				var localRes ioResult
				switch req.op {
				case opReadByte:
					b, err := c.reader.ReadByte()
					localRes = ioResult{data: b, err: err}
				case opReadString:
					s, err := c.reader.ReadString(req.delim)
					localRes = ioResult{data: s, err: err}
				case opReadAll:
					bytes, err := io.ReadAll(io.LimitReader(c.reader, req.limit))
					localRes = ioResult{data: bytes, err: err}
				}
				readDone <- localRes
			}(req)

			if req.ctx != nil {
				select {
				case res = <-readDone:
				case <-req.ctx.Done():
					res = ioResult{err: req.ctx.Err()}
					req.resCh <- res
					return // Exit worker to prevent data races on c.reader from leaked read goroutine
				}
			} else {
				res = <-readDone
			}

			req.resCh <- res
		}
	}()
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

// sendRequest enqueues a readRequest to the background worker and waits for
// the result, respecting context cancellation at both the send and receive stages.
func (c *capturer) sendRequest(ctx context.Context, req readRequest) (ioResult, error) {
	c.readerMu.Lock()
	if c.requestChan == nil {
		c.readerMu.Unlock()
		return ioResult{}, errCapturerClosed
	}
	select {
	case c.requestChan <- req:
		c.readerMu.Unlock()
	case <-ctx.Done():
		c.readerMu.Unlock()
		return ioResult{}, ctx.Err()
	}

	select {
	case <-ctx.Done():
		return ioResult{}, ctx.Err()
	case res := <-req.resCh:
		return res, nil
	}
}

// resolveInput determines the prompt text by reading from the appropriate input
// source (pipe or TTY) based on the current environment.
func (c *capturer) resolveInput(ctx context.Context, prompt string, options *ports.CaptureOptions) (string, error) {
	if !c.IsTTY(c.Stdin) {
		return c.captureFromPipe(ctx, prompt)
	}
	if prompt == "" && !options.SkipTTYWait {
		result, err := c.captureFromTTY(ctx, !options.Raw)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(result) == "" {
			return "", context.Canceled
		}
		return result, nil
	}
	return prompt, nil
}

// CapturePrompt captures the initial prompt from command line arguments or standard input.
func (c *capturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	options := &ports.CaptureOptions{}
	for _, opt := range opts {
		opt(options)
	}

	prompt := strings.Join(args, " ")

	if c.mockPrompt != "" {
		prompt = c.mockPrompt
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	if c.SM != nil {
		c.SM.TerminalLock()
		defer c.SM.TerminalUnlock()
	}

	prompt, err := c.resolveInput(ctx, prompt, options)
	if err != nil {
		return "", err
	}

	return c.finalizePrompt(prompt, options)
}

func (c *capturer) finalizePrompt(prompt string, options *ports.CaptureOptions) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		if options.SkipTTYWait {
			return "", ErrNoInput
		}
		_, _ = fmt.Fprintln(c.Stderr, "Usage: tell-me-go [flags] <prompt>")
		_, _ = fmt.Fprintln(c.Stderr, "Or use the interactive TUI prompt: tell-me-go -i")
		return "", fmt.Errorf("empty prompt")
	}

	c.printFeedback(c.Stderr, !options.Raw, colorGreen,
		fmt.Sprintf("[%s] Input captured. Processing...", c.Clock.Now().Format("15:04:05")))

	return prompt, nil
}

func (c *capturer) captureFromPipe(ctx context.Context, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	req := readRequest{op: opReadAll, limit: maxPromptSize, resCh: make(chan ioResult, 1), ctx: ctx}
	res, err := c.sendRequest(ctx, req)
	if err != nil {
		return "", err
	}
	if res.err != nil {
		return "", fmt.Errorf("failed to read from pipe: %w", res.err)
	}

	bytes := res.data.([]byte)
	combined := prompt
	if len(bytes) > 0 {
		combined = prompt + "\n" + string(bytes)
	}
	return combined, nil
}

func (c *capturer) captureFromTTY(ctx context.Context, useColor bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	c.printFeedback(c.Stdout, useColor, colorYellow, multiLineEOFHint)

	req := readRequest{op: opReadAll, limit: maxPromptSize, resCh: make(chan ioResult, 1), ctx: ctx}
	res, err := c.sendRequest(ctx, req)
	if err != nil {
		return "", err
	}
	if res.err != nil {
		return "", fmt.Errorf("failed to read from TTY: %w", res.err)
	}
	return string(res.data.([]byte)), nil
}

// printFeedback displays a message with optional color.
// It DOES NOT perform terminal locking to avoid deadlocks when called from security components.
func (c *capturer) printFeedback(w io.Writer, useColor bool, color, msg string) {
	if useColor && c.IsTTY(w) {
		prefix := "\r" + termClearLine
		if c.disableEscapeSequences {
			prefix = ""
		}
		_, _ = fmt.Fprintf(w, "%s%s%s%s\n", prefix, color, msg, colorReset)
	} else {
		_, _ = fmt.Fprintln(w, msg)
	}
}

// Confirm prompts the user for confirmation.
func (c *capturer) Confirm(ctx context.Context, message string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	color := ""
	if strings.HasPrefix(message, "[SECURITY]") || strings.HasPrefix(message, "[CONFIRMATION REQUIRED]") {
		color = colorRed
	}

	if c.IsTTY(c.Stderr) {
		prefix := "\r" + termClearLine
		if c.disableEscapeSequences {
			prefix = ""
		}
		if color != "" {
			_, _ = fmt.Fprintf(c.Stderr, "%s%s%s%s", prefix, color, message, colorReset)
		} else {
			_, _ = fmt.Fprintf(c.Stderr, "%s%s", prefix, message)
		}
	} else {
		_, _ = fmt.Fprint(c.Stderr, message)
	}

	char, err := c.ReadSingleKey(ctx)
	_, _ = fmt.Fprintf(c.Stderr, "\n")
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

	timestamp := c.Clock.Now().Format("15:04:05")
	formattedMessage := fmt.Sprintf("[%s] %s", timestamp, message)
	c.printFeedback(c.Stderr, true, color, formattedMessage)
}

// Prompt displays an inline message without a newline.
func (c *capturer) Prompt(message string) {
	if c.IsTTY(c.Stderr) {
		prefix := "\r" + termClearLine
		if c.disableEscapeSequences {
			prefix = ""
		}
		_, _ = fmt.Fprintf(c.Stderr, "%s%s%s%s", prefix, colorYellow, message, colorReset)
	} else {
		_, _ = fmt.Fprint(c.Stderr, message)
	}
}

// ReadSingleKey waits for a single key press from Stdin.
func (c *capturer) ReadSingleKey(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if c.mockAnswer != "" {
		return strings.ToLower(c.mockAnswer[:1]), nil
	}

	var fd int
	if f, ok := c.Stdin.(*os.File); ok {
		fd = int(f.Fd())
	} else {
		// Fallback for non-file readers in tests
		return c.readByteFallback(ctx)
	}

	if !c.IsTTY(c.Stdin) {
		if os.Getenv("GO_WANT_HELPER_PROCESS") != "" || c.disableEscapeSequences {
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
	if err := ctx.Err(); err != nil {
		return "", err
	}

	req := readRequest{op: opReadByte, resCh: make(chan ioResult, 1), ctx: ctx}
	res, err := c.sendRequest(ctx, req)
	if err != nil {
		return "", err
	}
	if res.err != nil {
		return "", res.err
	}
	b := res.data.(byte)
	if b == 3 { // Ctrl+C (ETX)
		return "", context.Canceled
	}
	return strings.ToLower(string(b)), nil
}

// ReadLine reads a line of input.
func (c *capturer) ReadLine(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	req := readRequest{op: opReadString, delim: '\n', resCh: make(chan ioResult, 1), ctx: ctx}
	res, err := c.sendRequest(ctx, req)
	if err != nil {
		return "", err
	}
	if res.err != nil {
		s := ""
		if res.data != nil {
			s = res.data.(string)
		}
		if res.err != io.EOF || s == "" {
			return "", res.err
		}
		return s, nil
	}
	return res.data.(string), nil
}

// Close gracefully stops the background worker.
func (c *capturer) Close(ctx context.Context) error {
	c.readerMu.Lock()
	if c.requestChan == nil {
		c.readerMu.Unlock()
		return nil
	}
	close(c.requestChan)
	c.requestChan = nil
	c.readerMu.Unlock()

	// Force interrupt any blocking reads by closing the underlying file.
	// This ensures the worker goroutine wakes up from the read() syscall.
	if closer, ok := c.Stdin.(io.Closer); ok {
		_ = closer.Close()
	}

	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
