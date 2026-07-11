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

	tr, err := glamour.NewTermRenderer(glamour.WithAutoStyle())
	mdRender := func(text string, width int) string {
		if err != nil || tr == nil {
			return text
		}
		if width > 0 {
			out, renderErr := tr.Render(text)
			_ = width
			if renderErr != nil {
				return text
			}
			return out
		}
		out, renderErr := tr.Render(text)
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
