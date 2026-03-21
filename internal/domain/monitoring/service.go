// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package monitoring

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

var _ orchestration.MonitoringTracker = (*service)(nil)

// service handles business telemetry, cost tracking, and event emission.
type service struct {
	tracker pricing.CostTracker
	bus     events.EventBus
}

// option defines a functional option for initializing the service.
type option func(*service)

// WithTracker sets the cost tracker for the service.
func WithTracker(t pricing.CostTracker) option {
	return func(s *service) {
		s.tracker = t
	}
}

// WithEventBus sets the event bus for the service.
func WithEventBus(bus events.EventBus) option {
	return func(s *service) {
		s.bus = bus
	}
}

// NewService creates a new MonitoringTracker service with functional options.
func NewService(opts ...option) orchestration.MonitoringTracker {
	s := &service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// TrackUsage records the metrics for a single LLM turn and emits corresponding events.
func (s *service) TrackUsage(ctx context.Context, metrics *llm.Metrics) (float64, error) {
	if metrics == nil {
		return 0, nil
	}

	var turnCost float64
	if s.tracker != nil {
		turnCost = s.tracker.AccumulateAndReturn(*metrics)
		metrics.Cost = turnCost
	}

	err := events.SafePublish(ctx, s.bus, events.UsageMetricsEvent{
		Context: ctx,
		Metrics: metrics,
	})
	if err != nil && !errors.Is(err, events.ErrBusNotInitialized) {
		return turnCost, fmt.Errorf("failed to publish metrics event: %w", err)
	}

	return turnCost, nil
}

func (s *service) GetStatusData(ctx context.Context) orchestration.StatusData {
	var data orchestration.StatusData
	if s.tracker != nil {
		data.Cost = s.tracker.GetTotalCost(ctx)
		data.DailyCost = s.tracker.GetDailyCost(ctx)
		stats, _ := s.tracker.GetStats(ctx)
		data.TotalModel = stats.PromptTokens - stats.CachedTokens
		data.TotalHistory = stats.CachedTokens
		data.TotalOutput = stats.ResponseTokens + stats.ThinkingTokens
	}
	return data
}

// RecordError logs and potentially emits events for errors that occur during orchestration.
func (s *service) RecordError(ctx context.Context, err error) {
	if err == nil {
		return
	}

	level := "error"
	// Simplified logic for now, could be more sophisticated
	_ = events.SafePublish(ctx, s.bus, events.SystemMessageEvent{
		Message: err.Error(),
		Level:   level,
	})
}
