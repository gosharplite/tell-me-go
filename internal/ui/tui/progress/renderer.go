// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type renderer struct {
	metricsProvider ports.SystemMetricsProvider
}

// NewRenderer creates a ProgressRenderer backed by the Bubble Tea progress model.
func NewRenderer(metricsProvider ports.SystemMetricsProvider) ports.ProgressRenderer {
	return &renderer{metricsProvider: metricsProvider}
}

func (r *renderer) Run(ctx context.Context, source ports.EventSubscriber) func() {
	ch := make(chan events.Event, 256)

	source.Subscribe(r.makeSubscriber(ch))

	m := NewModel(ctx, ch, r.metricsProvider)
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
			// Control-plane events. A 5-second deadline prevents
			// backpressure deadlock when the TUI channel is full
			// (e.g., slow Glamour rendering). The subscriber loop's
			// 30s timeout ctx is the ultimate backstop.
			timer := time.NewTimer(5 * time.Second)
			select {
			case ch <- e:
				timer.Stop()
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
			}
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
