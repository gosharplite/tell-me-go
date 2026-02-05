// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
	"golang.org/x/term"
)

func (a *App) capturePrompt(ctx context.Context, fs *flag.FlagSet, lastN int) (string, error) {
	prompt := strings.Join(fs.Args(), " ")

	// Support for E2E mocking of user input
	if val := os.Getenv("TELL_ME_MOCK_PROMPT"); val != "" {
		return val, nil
	}

	var fd int = -1
	if f, ok := a.Stdin.(*os.File); ok {
		fd = int(f.Fd())
	}
	isTerminal := fd != -1 && term.IsTerminal(fd)

	if !isTerminal {
		// Non-terminal: Read all available input (e.g., piped content)
		readChan := make(chan []byte, 1)
		go func() {
			// Note: This blocks on the underlying read. While the context allows
			// the main loop to continue, this goroutine remains until EOF.
			b, _ := io.ReadAll(a.Stdin)
			readChan <- b
		}()

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case b := <-readChan:
			if len(b) > 0 {
				if prompt != "" {
					prompt = prompt + "\n" + string(b)
				} else {
					prompt = string(b)
				}
			}
		}
	} else if prompt == "" && lastN == 0 {
		func() {
			a.sm.TerminalLock()
			defer a.sm.TerminalUnlock()
			fmt.Fprintf(a.Stdout, "%s[Reading multi-line input. Press Ctrl+D to send]%s\n", colors.ColorYellow, colors.ColorReset)
		}()

		// Terminal: Read multi-line input until EOF (Ctrl+D)
		readChan := make(chan []byte, 1)
		go func() {
			// Note: This blocks on the underlying read. While the context allows
			// the main loop to continue, this goroutine remains until EOF.
			b, _ := io.ReadAll(a.Stdin)
			readChan <- b
		}()

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case b := <-readChan:
			prompt = string(b)
		}
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		if lastN > 0 {
			return "", nil // Valid case if just showing history
		}
		fmt.Fprintln(a.Stderr, "Usage: tell-me-go [flags] <prompt>")
		fs.PrintDefaults()
		return "", fmt.Errorf("empty prompt")
	}
	func() {
		a.sm.TerminalLock()
		defer a.sm.TerminalUnlock()
		fmt.Fprintf(a.Stderr, "%s[%s] Input captured. Processing...%s\n", colors.ColorGreen, time.Now().Format("15:04:05"), colors.ColorReset)
	}()
	return prompt, nil
}

func (a *App) showHistory(hManager *history.Manager, n int, raw bool, showThoughts bool) {
	contents := hManager.GetContents()
	if len(contents) == 0 {
		fmt.Fprintln(a.Stdout, "No history found.")
		return
	}

	if n > len(contents) {
		n = len(contents)
	}

	start := len(contents) - n
	var r *glamour.TermRenderer
	if !raw {
		r, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithEmoji(),
		)
	}

	for i := start; i < len(contents); i++ {
		c := contents[i]
		roleColor := colors.ColorBlue // Blue for User
		if c.Role != "user" {
			roleColor = colors.ColorMagenta // Magenta for Model
		}
		fmt.Fprintf(a.Stdout, "%s[%s]%s\n", roleColor, strings.ToUpper(c.Role), colors.ColorReset)
		for _, p := range c.Parts {
			// Skip thinking parts if the user hasn't enabled them in config
			if p.Thought && !showThoughts {
				continue
			}

			if p.Text != "" {
				if raw || r == nil {
					fmt.Fprint(a.Stdout, p.Text)
					if !strings.HasSuffix(p.Text, "\n") {
						fmt.Fprintln(a.Stdout)
					}
				} else {
					out, err := r.Render(p.Text)
					if err != nil {
						fmt.Fprintln(a.Stdout, p.Text)
					} else {
						fmt.Fprint(a.Stdout, out)
					}
				}
			}
			if p.FunctionCall != nil {
				fmt.Fprintf(a.Stdout, "%s[Tool Call] %s%s\n", colors.ColorCyan, p.FunctionCall.Name, colors.ColorReset)
			}
			if p.FunctionResponse != nil {
				fmt.Fprintf(a.Stdout, "%s[Tool Response] %s%s\n", colors.ColorCyan, p.FunctionResponse.Name, colors.ColorReset)
			}
		}
		fmt.Fprintln(a.Stdout)
	}
}
