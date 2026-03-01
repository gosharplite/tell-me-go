// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package monitoring

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockCostTracker is a mock for pricing.ICostTracker.
type mockCostTracker struct {
	mock.Mock
}

var _ pricing.ICostTracker = (*mockCostTracker)(nil)

func (m *mockCostTracker) GetTotalCost(ctx context.Context) float64 {
	return m.Called(ctx).Get(0).(float64)
}

func (m *mockCostTracker) GetDailyCost(ctx context.Context) float64 {
	return m.Called(ctx).Get(0).(float64)
}

func (m *mockCostTracker) GetStats(ctx context.Context) (pricing.UsageStats, float64) {
	args := m.Called(ctx)
	return args.Get(0).(pricing.UsageStats), args.Get(1).(float64)
}

func (m *mockCostTracker) Accumulate(mt llm.Metrics) {
	m.Called(mt)
}

func (m *mockCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	return m.Called(mt).Get(0).(float64)
}

func (m *mockCostTracker) Warmup() {
	m.Called()
}

// mockEventBus is a mock for events.EventBus.
type mockEventBus struct {
	mock.Mock
}

var _ events.EventBus = (*mockEventBus)(nil)

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error {
	return m.Called(ctx, e).Error(0)
}

func (m *mockEventBus) Subscribe(sub func(events.Event)) {
	m.Called(sub)
}

func (m *mockEventBus) Shutdown(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockEventBus) Flush(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func TestTrackUsage(t *testing.T) {
	tests := []struct {
		name          string
		metrics       *llm.Metrics
		setupMock     func(mt *mockCostTracker, eb *mockEventBus)
		expectedError string
	}{
		{
			name: "Happy path - Successful cost tracking and event emission",
			metrics: &llm.Metrics{Model: "gpt-4"},
			setupMock: func(mt *mockCostTracker, eb *mockEventBus) {
				mt.On("AccumulateAndReturn", mock.Anything).Return(1.0)
				eb.On("Publish", mock.Anything, mock.MatchedBy(func(e events.UsageMetricsEvent) bool {
					return e.Metrics.Cost == 1.0
				})).Return(nil)
			},
			expectedError: "",
		},
		{
			name: "EventBus error",
			metrics: &llm.Metrics{Model: "gpt-4"},
			setupMock: func(mt *mockCostTracker, eb *mockEventBus) {
				mt.On("AccumulateAndReturn", mock.Anything).Return(1.0)
				eb.On("Publish", mock.Anything, mock.Anything).Return(errors.New("event error"))
			},
			expectedError: "event error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mt := &mockCostTracker{}
			eb := &mockEventBus{}
			tt.setupMock(mt, eb)
			service := NewService(WithTracker(mt), WithEventBus(eb))

			_, err := service.TrackUsage(context.Background(), tt.metrics)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
			mt.AssertExpectations(t)
			eb.AssertExpectations(t)
		})
	}
}

func TestRecordError(t *testing.T) {
	eb := &mockEventBus{}
	eb.On("Publish", mock.Anything, mock.MatchedBy(func(e events.SystemMessageEvent) bool {
		return e.Message == "some error" && e.Level == "error"
	})).Return(nil)

	service := NewService(WithEventBus(eb))
	service.RecordError(context.Background(), errors.New("some error"))

	eb.AssertExpectations(t)
}
