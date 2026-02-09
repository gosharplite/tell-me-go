// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// Renderer defines the contract for displaying agent state, status updates, and usage metrics.
type Renderer interface {
	RenderStatus(ctx context.Context, status events.TurnStatus)
	RenderEvent(ctx context.Context, event events.Event)
}
