// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// EventSubscriber is implemented by types that can subscribe to domain events.
type EventSubscriber interface {
	Subscribe(handler func(context.Context, events.Event))
}

// ProgressRenderer runs a progress display that consumes events from an
// EventSubscriber. Run returns a cleanup function that must be called to
// signal the renderer to exit gracefully.
type ProgressRenderer interface {
	Run(ctx context.Context, source EventSubscriber) (cleanup func())
}
