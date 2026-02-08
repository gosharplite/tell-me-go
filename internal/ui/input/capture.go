// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package input

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
	"golang.org/x/term"
)

// Capturer handles capturing user input from TTY or pipes.
type Capturer struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	SM     security.ISecurityManager
}

// NewCapturer creates a new Capturer.
func NewCapturer(stdin io.Reader, stdout, stderr io.Writer, sm security.ISecurityManager) *Capturer {
	return &Capturer{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		SM:     sm,
	}
}

// IsTTY returns true if the value (usually an *os.File) is a terminal.
func (c *Capturer) IsTTY(v any) bool {
	if f, ok := v.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// Prompt captures the initial prompt from command line arguments or standard input.
func (c *Capturer) Prompt(ctx context.Context, fs *flag.FlagSet, lastN int, raw bool) (string, error) {
	prompt := strings.Join(fs.Args(), " ")

	if val := os.Getenv("TELL_ME_MOCK_PROMPT"); val != "" {
		return val, nil
	}

	var err error
	if !c.IsTTY(c.Stdin) {
		prompt, err = c.captureFromPipe(ctx, prompt)
	} else if prompt == "" && lastN == 0 {
		prompt, err = c.captureFromTTY(ctx, !raw)
	}

	if err != nil {
		return "", err
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		if lastN > 0 {
			return "", nil
		}
		fmt.Fprintln(c.Stderr, "Usage: tell-me-go [flags] <prompt>")
		fs.PrintDefaults()
		return "", fmt.Errorf("empty prompt")
	}

	c.PrintFeedback(c.Stderr, !raw, colors.ColorGreen,
		fmt.Sprintf("[%s] Input captured. Processing...", time.Now().Format("15:04:05")))

	return prompt, nil
}

func (c *Capturer) captureFromPipe(ctx context.Context, initialPrompt string) (string, error) {
	readChan := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(c.Stdin)
		readChan <- b
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case b := <-readChan:
		if len(b) == 0 {
			return initialPrompt, nil
		}
		if initialPrompt != "" {
			return initialPrompt + "\n" + string(b), nil
		}
		return string(b), nil
	}
}

func (c *Capturer) captureFromTTY(ctx context.Context, useColor bool) (string, error) {
	c.PrintFeedback(c.Stdout, useColor, colors.ColorYellow, "[Reading multi-line input. Press Ctrl+D to send]")

	readChan := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(c.Stdin)
		readChan <- b
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case b := <-readChan:
		return string(b), nil
	}
}

// PrintFeedback displays a message with optional color and locking.
func (c *Capturer) PrintFeedback(w io.Writer, useColor bool, color, msg string) {
	if c.SM != nil {
		c.SM.TerminalLock()
		defer c.SM.TerminalUnlock()
	}
	if useColor && c.IsTTY(w) {
		fmt.Fprintf(w, "%s%s%s\n", color, msg, colors.ColorReset)
	} else {
		fmt.Fprintln(w, msg)
	}
}
