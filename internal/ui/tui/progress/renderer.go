// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type renderer struct{}

// NewRenderer creates a ProgressRenderer backed by the Bubble Tea progress model.
func NewRenderer() ports.ProgressRenderer {
	return &renderer{}
}

func (r *renderer) Run(ctx context.Context, source ports.EventSubscriber) func() {
	ch := make(chan events.Event, 64)

	source.Subscribe(r.makeSubscriber(ch))

	mdRender := r.makeMarkdownRenderer()

	m := NewModel(ctx, ch, mdRender)
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithAltScreen())

	done := make(chan struct{})
	go func() {
		_, _ = p.Run()
		close(done)
	}()

	return func() {
		close(ch) // signal session done to model
		<-done    // wait for user to dismiss TUI (Ctrl+C or q)
	}
}

// makeSubscriber returns an event subscriber callback that writes events
// to the channel. High-priority events use a 100ms deadline; all others
// use a non-blocking send.
func (r *renderer) makeSubscriber(ch chan<- events.Event) func(context.Context, events.Event) {
	return func(ctx context.Context, e events.Event) {
		switch e.(type) {
		case events.TurnStarted, events.TurnStatusEvent, events.ResponseEvent:
			select {
			case ch <- e:
			case <-time.After(100 * time.Millisecond):
			}
		default:
			select {
			case ch <- e:
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
}

// makeMarkdownRenderer returns a markdown-to-ANSI render function that
// caches the glamour TermRenderer and re-creates it on width changes.
func (r *renderer) makeMarkdownRenderer() func(string, int) string {
	var cachedRenderer *glamour.TermRenderer
	var lastWidth int
	return func(text string, width int) string {
		if width != lastWidth || cachedRenderer == nil {
			opts := []glamour.TermRendererOption{glamour.WithAutoStyle()}
			if width > 0 {
				opts = append(opts, glamour.WithWordWrap(width))
			}
			tr, renderErr := glamour.NewTermRenderer(opts...)
			if renderErr != nil {
				return text
			}
			cachedRenderer = tr
			lastWidth = width
		}
		out, renderErr := cachedRenderer.Render(text)
		if renderErr != nil {
			return text
		}
		return out
	}
}
