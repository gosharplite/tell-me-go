// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// StdHistoryRenderer implements services.HistoryRenderer by wrapping the package-level History function.
type StdHistoryRenderer struct{}

// Render implements services.HistoryRenderer.
func (r *StdHistoryRenderer) Render(w io.Writer, h services.HistoryManager, n int, options services.HistoryRenderOptions) {
	renderHistory(w, h, n, options)
}

// renderHistory renders the chat history to the provided writer.
func renderHistory(w io.Writer, h services.HistoryManager, n int, options services.HistoryRenderOptions) {
	contents := h.GetContents()
	if len(contents) == 0 {
		fmt.Fprintln(w, "No history found.")
		return
	}

	if n > len(contents) {
		n = len(contents)
	}

	start := len(contents) - n
	hr := &historyRenderer{
		writer:       w,
		raw:          options.Raw,
		showThoughts: options.ShowThoughts,
		useColor:     options.UseColor,
	}
	if !options.Raw {
		hr.renderer, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithEmoji(),
		)
	}

	for i := start; i < len(contents); i++ {
		content := contents[i]
		hr.renderHeader(content.Role)

		for _, p := range content.Parts {
			if p != nil {
				hr.renderPart(*p)
			}
		}
		fmt.Fprintln(w)
	}
}

type historyRenderer struct {
	renderer     *glamour.TermRenderer
	writer       io.Writer
	raw          bool
	showThoughts bool
	useColor     bool
}

func (r *historyRenderer) renderHeader(role string) {
	roleStr := "[" + strings.ToUpper(role) + "]"
	if r.useColor {
		roleColor := colorBlue
		if role != "user" {
			roleColor = colorMagenta
		}
		fmt.Fprintf(r.writer, "%s%s%s\n", roleColor, roleStr, colorReset)
	} else {
		fmt.Fprintln(r.writer, roleStr)
	}
}

func (r *historyRenderer) renderText(text string) {
	if r.raw || r.renderer == nil {
		fmt.Fprint(r.writer, text)
		if !strings.HasSuffix(text, "\n") {
			fmt.Fprintln(r.writer)
		}
	} else {
		out, err := r.renderer.Render(text)
		if err != nil {
			fmt.Fprintln(r.writer, text)
		} else {
			fmt.Fprint(r.writer, out)
		}
	}
}

func (r *historyRenderer) renderPart(p llm.Part) {
	if p.Thought && !r.showThoughts {
		return
	}

	if p.Text != "" {
		r.renderText(p.Text)
	}
	if p.FunctionCall != nil {
		if r.useColor {
			fmt.Fprintf(r.writer, "%s[Tool Call] %s%s\n", colorCyan, p.FunctionCall.Name, colorReset)
		} else {
			fmt.Fprintf(r.writer, "[Tool Call] %s\n", p.FunctionCall.Name)
		}
	}
	if p.FunctionResponse != nil {
		if r.useColor {
			fmt.Fprintf(r.writer, "%s[Tool Response] %s%s\n", colorCyan, p.FunctionResponse.Name, colorReset)
		} else {
			fmt.Fprintf(r.writer, "[Tool Response] %s\n", p.FunctionResponse.Name)
		}
	}
}
