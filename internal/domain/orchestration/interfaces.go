// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ContextPreparationService defines the interface for preparing LLM context.
type ContextPreparationService interface {
	// Prepare gathers and optimizes history for the next LLM turn.
	Prepare(ctx context.Context, turn int) ([]*llm.Content, int, error)
	// AddContent appends new content to the current session history.
	AddContent(ctx context.Context, content *llm.Content) error
	// GetMetadata returns diagnostics and auxiliary data from the last preparation.
	// This is optional but useful for the MonitoringTracker.
	// For now, we'll keep the interface simple and add a wrapper if needed.
}

// ExecutionOrchestrator defines the interface for executing tools and managing turn state.
type ExecutionOrchestrator interface {
	// Execute takes the model response, identifies tool calls, executes them,
	// and returns the combined tool results as a new Content object.
	Execute(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error)
}

// LLMCoordinator handles the abstraction of the LLM Gateway, provider selection, and streaming.
type LLMCoordinator interface {
	// Generate generates a response from the LLM based on the provided history and tools.
	// It handles the streaming and returns the final content and metrics.
	Generate(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

// MonitoringTracker handles business telemetry, cost tracking, and event emission.
type MonitoringTracker interface {
	// TrackUsage records the metrics for a single LLM turn and emits corresponding events.
	TrackUsage(ctx context.Context, metrics *llm.Metrics) (float64, error)
	// RecordError logs and potentially emits events for errors that occur during orchestration.
	RecordError(ctx context.Context, err error)
	GetStatusData(ctx context.Context) (cost, dailyCost float64, totalM, totalH, totalO int64)
}
