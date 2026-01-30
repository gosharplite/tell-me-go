// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/history"
)

func (a *App) capturePrompt(fs *flag.FlagSet, lastN int) (string, error) {
	prompt := strings.Join(fs.Args(), " ")
	var isTerminal bool
	if f, ok := a.Stdin.(*os.File); ok {
		stat, _ := f.Stat()
		isTerminal = (stat.Mode() & os.ModeCharDevice) != 0
	} else {
		isTerminal = false // Assume non-terminal for non-file readers (like buffers in tests)
	}

	if !isTerminal {
		b, err := io.ReadAll(a.Stdin)
		if err == nil && len(b) > 0 {
			if prompt != "" {
				prompt = prompt + "\n" + string(b)
			} else {
				prompt = string(b)
			}
		}
	} else if prompt == "" && lastN == 0 {
		fmt.Fprintln(a.Stdout, "\033[0;33m[Reading multi-line input. Press Ctrl+D to send]\033[0m")
		b, err := io.ReadAll(a.Stdin)
		if err == nil {
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
		fmt.Fprintf(a.Stderr, "\033[0;32m[%s] Input captured. Processing...\033[0m\n", time.Now().Format("15:04:05"))
	}()
	return prompt, nil
}

func (a *App) showHistory(hManager *history.Manager, n int, raw bool) {
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
		roleColor := "\033[1;34m" // Blue for User
		if c.Role != "user" {
			roleColor = "\033[1;35m" // Magenta for Model
		}
		fmt.Fprintf(a.Stdout, "%s[%s]%s\n", roleColor, strings.ToUpper(c.Role), "\033[0m")
		for _, p := range c.Parts {
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
				fmt.Fprintf(a.Stdout, "\033[0;36m[Tool Call] %s\033[0m\n", p.FunctionCall.Name)
			}
			if p.FunctionResponse != nil {
				fmt.Fprintf(a.Stdout, "\033[0;36m[Tool Response] %s\033[0m\n", p.FunctionResponse.Name)
			}
		}
		fmt.Fprintln(a.Stdout)
	}
}
