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

	source.Subscribe(func(ctx context.Context, e events.Event) {
		switch e.(type) {
		case events.TurnStarted, events.TurnStatusEvent, events.ResponseEvent:
			select {
			case ch <- e:
			case <-time.After(100 * time.Millisecond):
			}
		default:
			select {
			case ch <- e:
			default:
			}
		}
	})

	var cachedRenderer *glamour.TermRenderer
	var lastWidth int

	mdRender := func(text string, width int) string {
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
	m := NewModel(ctx, ch, mdRender)
	p := tea.NewProgram(m, tea.WithInput(nil))
	go func() {
		_, _ = p.Run()
	}()

	return func() { close(ch) }
}
