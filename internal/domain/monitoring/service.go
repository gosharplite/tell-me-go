// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package monitoring

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

var _ orchestration.MonitoringTracker = (*Service)(nil)

// Service handles business telemetry, cost tracking, and event emission.
type Service struct {
	mu      sync.RWMutex
	tracker pricing.ICostTracker
	bus     events.EventBus
}

// Option defines a functional option for initializing the Service.
type Option func(*Service)

// WithTracker sets the cost tracker for the service.
func WithTracker(t pricing.ICostTracker) Option {
	return func(s *Service) {
		s.tracker = t
	}
}

// WithEventBus sets the event bus for the service.
func WithEventBus(bus events.EventBus) Option {
	return func(s *Service) {
		s.bus = bus
	}
}

// NewService creates a new MonitoringTracker service with functional options.
func NewService(opts ...Option) *Service {
	s := &Service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// TrackUsage records the metrics for a single LLM turn and emits corresponding events.
func (s *Service) TrackUsage(ctx context.Context, metrics *llm.Metrics) error {
	if metrics == nil {
		return nil
	}

	var cost float64
	if s.tracker != nil {
		cost = s.tracker.AccumulateAndReturn(*metrics)
		metrics.Cost = cost
	}

	if s.bus != nil {
		err := s.bus.Publish(ctx, events.UsageMetricsEvent{
			Context: ctx,
			Metrics: metrics,
		})
		if err != nil {
			return fmt.Errorf("failed to publish metrics event: %w", err)
		}
	}

	return nil
}

// RecordError logs and potentially emits events for errors that occur during orchestration.
func (s *Service) RecordError(ctx context.Context, err error) {
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
