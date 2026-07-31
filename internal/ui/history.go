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
func (r *StdHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
	renderHistory(w, h, n, options)
}

// renderHistory renders the chat history to the provided writer.
func renderHistory(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
	total := h.GetTotalEntries()
	if total == 0 {
		_, _ = fmt.Fprintln(w, "No history found.")
		return
	}

	if n > total {
		n = total
	}

	start := total - n
	contents, err := h.GetWindow(context.Background(), start, -1)
	if err != nil {
		writeBestEffort(w, "Error retrieving history: %v\n", err)
		return
	}

	hr := &historyRenderer{
		writer:        w,
		raw:           options.Raw,
		showThoughts:  options.ShowThoughts,
		useColor:      options.UseColor,
		suppressRoles: options.SuppressRoles,
	}
	if !options.Raw {
		if options.CustomRenderer != nil {
			hr.renderer = options.CustomRenderer
		} else {
			hr.renderer, _ = glamour.NewTermRenderer(
				glamour.WithStandardStyle(resolveGlamourStyle()),
				glamour.WithEmoji(),
			)
		}
	}

	for _, content := range contents {
		hr.renderHeader(content.Role)

		// When thoughts are hidden but there is no non-thought visible
		// text, fall back to showing thought content. Reasoning models
		// may put the entire answer in reasoning_content with content=null.
		effectiveShowThoughts := hr.showThoughts
		if !effectiveShowThoughts {
			hasNonThought := false
			for _, p := range content.Parts {
				if p != nil && p.Text != "" && !p.IsThought {
					hasNonThought = true
					break
				}
			}
			if !hasNonThought {
				effectiveShowThoughts = true
			}
		}

		for _, p := range content.Parts {
			if p != nil {
				hr.renderPart(*p, effectiveShowThoughts)
			}
		}
		_, _ = fmt.Fprintln(w)
	}
}

type historyRenderer struct {
	renderer      markdownRenderer
	writer        io.Writer
	raw           bool
	showThoughts  bool
	useColor      bool
	suppressRoles bool
}

func (r *historyRenderer) renderHeader(role string) {
	if r.suppressRoles {
		return
	}
	roleStr := "[" + strings.ToUpper(role) + "]"
	if r.useColor {
		roleColor := colorBlue
		if role != "user" {
			roleColor = colorMagenta
		}
		writeBestEffort(r.writer, "%s%s%s\n", roleColor, roleStr, colorReset)
	} else {
		_, _ = fmt.Fprintln(r.writer, roleStr)
	}
}

func (r *historyRenderer) renderText(text string) {
	if r.raw || r.renderer == nil {
		_, _ = fmt.Fprint(r.writer, text)
		if !strings.HasSuffix(text, "\n") {
			_, _ = fmt.Fprintln(r.writer)
		}
	} else {
		out, err := r.renderer.Render(text)
		if err != nil {
			// Surface render error in output so the user can see degradation
			writeBestEffort(r.writer, "\n[render error: %v]\n", err)
			_, _ = fmt.Fprintln(r.writer, text)
		} else {
			_, _ = fmt.Fprint(r.writer, out)
		}
	}
}

func (r *historyRenderer) renderPart(p llm.Part, showThoughts bool) {
	if p.IsThought && !showThoughts {
		return
	}

	text := p.Text

	if text != "" {
		r.renderText(text)
	}
	if p.FunctionCall != nil {
		if r.useColor {
			writeBestEffort(r.writer, "%s[Tool Call] %s%s\n", colorCyan, p.FunctionCall.Name, colorReset)
		} else {
			writeBestEffort(r.writer, "[Tool Call] %s\n", p.FunctionCall.Name)
		}
	}
	if p.FunctionResponse != nil {
		if r.useColor {
			writeBestEffort(r.writer, "%s[Tool Response] %s%s\n", colorCyan, p.FunctionResponse.Name, colorReset)
		} else {
			writeBestEffort(r.writer, "[Tool Response] %s\n", p.FunctionResponse.Name)
		}
	}
}
