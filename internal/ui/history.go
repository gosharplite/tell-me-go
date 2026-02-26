// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// StdHistoryRenderer implements ports.HistoryRenderer by wrapping the package-level History function.
type StdHistoryRenderer struct{}

// Render implements ports.HistoryRenderer.
func (r *StdHistoryRenderer) Render(w io.Writer, h ports.HistoryManager, n int, options ports.HistoryRenderOptions) {
	renderHistory(w, h, n, options)
}

// renderHistory renders the chat history to the provided writer.
func renderHistory(w io.Writer, h ports.HistoryManager, n int, options ports.HistoryRenderOptions) {
	total := h.GetTotalEntries()
	if total == 0 {
		fmt.Fprintln(w, "No history found.")
		return
	}

	if n > total {
		n = total
	}

	start := total - n
	contents, err := h.GetWindow(context.Background(), start, -1)
	if err != nil {
		fmt.Fprintf(w, "Error retrieving history: %v\n", err)
		return
	}

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

	for _, content := range contents {
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
	if p.IsThought && !r.showThoughts {
		return
	}

	text := p.Text

	if text != "" {
		r.renderText(text)
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
