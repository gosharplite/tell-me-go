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
	ch := make(chan events.Event, 256)

	source.Subscribe(r.makeSubscriber(ch))

	mdRender := r.makeMarkdownRenderer()

	m := NewModel(ctx, ch, mdRender)
	p := tea.NewProgram(m, tea.WithAltScreen())

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
// to the channel. Control-plane events (TurnStarted, TurnStatusEvent) block
// until the TUI drains the channel — they must never be dropped. ResponseEvent
// uses a 100ms deadline; all others use a 50ms deadline (best-effort).
func (r *renderer) makeSubscriber(ch chan<- events.Event) func(context.Context, events.Event) {
	return func(ctx context.Context, e events.Event) {
		switch e.(type) {
		case events.TurnStarted, events.TurnStatusEvent:
			// Control-plane events — must never drop. The TUI cannot
			// track turn progress without these. Blocks the agent's
			// event publisher until the TUI drains the channel.
			ch <- e
		case events.ResponseEvent:
			// Display-plane event that can be regenerated from history.
			// Use deadline to avoid blocking the agent on slow TUI.
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case ch <- e:
				timer.Stop()
			case <-timer.C:
			}
		default:
			// Tool call/result/output events — best-effort, can drop.
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case ch <- e:
				timer.Stop()
			case <-timer.C:
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
				// glamour.NewTermRenderer only fails on invalid options or
				// internal library errors, not on valid markdown input. The
				// fallback returns raw unrendered text as a cosmetic degrade.
				// Coverage gap accepted by architect — structurally unreachable.
				return text
			}
			cachedRenderer = tr
			lastWidth = width
		}
		out, renderErr := cachedRenderer.Render(text)
		if renderErr != nil {
			// glamour.TermRenderer.Render only fails on internal rendering
			// errors, not on valid markdown input. The fallback returns raw
			// unrendered text as a cosmetic degrade.
			// Coverage gap accepted by architect — structurally unreachable.
			return text
		}
		return out
	}
}
