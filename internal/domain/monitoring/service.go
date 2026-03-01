// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package monitoring

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

var _ orchestration.MonitoringTracker = (*service)(nil)

// service handles business telemetry, cost tracking, and event emission.
type service struct {
	tracker pricing.ICostTracker
	bus     events.EventBus
}

// option defines a functional option for initializing the service.
type option func(*service)

// WithTracker sets the cost tracker for the service.
func WithTracker(t pricing.ICostTracker) option {
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
func (s *service) TrackUsage(ctx context.Context, metrics *llm.Metrics) error {
	if metrics == nil {
		return nil
	}

	// Create a shallow copy to avoid mutating the caller's pointer
	metricsCopy := *metrics

	if s.tracker != nil {
		metricsCopy.Cost = s.tracker.AccumulateAndReturn(metricsCopy)
	}

	if s.bus != nil {
		err := s.bus.Publish(ctx, events.UsageMetricsEvent{
			Context: ctx,
			Metrics: &metricsCopy,
		})
		if err != nil {
			return fmt.Errorf("failed to publish metrics event: %w", err)
		}
	}

	return nil
}

// RecordError logs and potentially emits events for errors that occur during orchestration.
func (s *service) RecordError(ctx context.Context, err error) {
	if err == nil || s.bus == nil {
		return
	}

	level := "error"
	// Simplified logic for now, could be more sophisticated
	_ = s.bus.Publish(ctx, events.SystemMessageEvent{
		Message: err.Error(),
		Level:   level,
	})
}
