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
		withTracker   bool
		withBus       bool
	}{
		{
			name:    "Happy path - Successful cost tracking and event emission",
			metrics: &llm.Metrics{Model: "gpt-4"},
			setupMock: func(mt *mockCostTracker, eb *mockEventBus) {
				mt.On("AccumulateAndReturn", mock.Anything).Return(1.0)
				eb.On("Publish", mock.Anything, mock.MatchedBy(func(e events.UsageMetricsEvent) bool {
					return e.Metrics.Cost == 1.0
				})).Return(nil)
			},
			expectedError: "",
			withTracker:   true,
			withBus:       true,
		},
		{
			name:    "EventBus error",
			metrics: &llm.Metrics{Model: "gpt-4"},
			setupMock: func(mt *mockCostTracker, eb *mockEventBus) {
				mt.On("AccumulateAndReturn", mock.Anything).Return(1.0)
				eb.On("Publish", mock.Anything, mock.Anything).Return(errors.New("event error"))
			},
			expectedError: "event error",
			withTracker:   true,
			withBus:       true,
		},
		{
			name:    "Nil metrics",
			metrics: nil,
			setupMock: func(mt *mockCostTracker, eb *mockEventBus) {
				// No mocks needed
			},
			expectedError: "",
			withTracker:   true,
			withBus:       true,
		},
		{
			name:    "Nil tracker and nil bus",
			metrics: &llm.Metrics{Model: "gpt-4"},
			setupMock: func(mt *mockCostTracker, eb *mockEventBus) {
				// No mocks needed
			},
			expectedError: "",
			withTracker:   false,
			withBus:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mt := &mockCostTracker{}
			eb := &mockEventBus{}
			tt.setupMock(mt, eb)

			var opts []option
			if tt.withTracker {
				opts = append(opts, WithTracker(mt))
			}
			if tt.withBus {
				opts = append(opts, WithEventBus(eb))
			}
			service := NewService(opts...)

			_, err := service.TrackUsage(context.Background(), tt.metrics)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
			if tt.withTracker {
				mt.AssertExpectations(t)
			}
			if tt.withBus {
				eb.AssertExpectations(t)
			}
		})
	}
}

func TestRecordError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		withBus   bool
		setupMock func(eb *mockEventBus)
	}{
		{
			name:    "Normal error with bus",
			err:     errors.New("some error"),
			withBus: true,
			setupMock: func(eb *mockEventBus) {
				eb.On("Publish", mock.Anything, mock.MatchedBy(func(e events.SystemMessageEvent) bool {
					return e.Message == "some error" && e.Level == "error"
				})).Return(nil)
			},
		},
		{
			name:    "Nil error",
			err:     nil,
			withBus: true,
			setupMock: func(eb *mockEventBus) {
				// No publish expected
			},
		},
		{
			name:    "Error without bus",
			err:     errors.New("some error"),
			withBus: false,
			setupMock: func(eb *mockEventBus) {
				// No publish expected
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eb := &mockEventBus{}
			tt.setupMock(eb)

			var opts []option
			if tt.withBus {
				opts = append(opts, WithEventBus(eb))
			}
			service := NewService(opts...)

			service.RecordError(context.Background(), tt.err)

			if tt.withBus {
				eb.AssertExpectations(t)
			}
		})
	}
}

func TestService_GetStatusData(t *testing.T) {
	tests := []struct {
		name            string
		withTracker     bool
		setupMock       func(mt *mockCostTracker)
		expectedCost    float64
		expectedDaily   float64
		expectedTotalM  int64
		expectedTotalH  int64
		expectedTotalO  int64
	}{
		{
			name:        "Tracker available",
			withTracker: true,
			setupMock: func(mt *mockCostTracker) {
				mt.On("GetTotalCost", mock.Anything).Return(150.0)
				mt.On("GetDailyCost", mock.Anything).Return(10.0)
				mt.On("GetStats", mock.Anything).Return(pricing.UsageStats{
					PromptTokens:    1000,
					CachedTokens:    200,
					ResponseTokens:  300,
					ThinkingTokens:  50,
				}, 150.0)
			},
			expectedCost:   150.0,
			expectedDaily:  10.0,
			expectedTotalM: 800, // 1000 - 200
			expectedTotalH: 200, // 200
			expectedTotalO: 350, // 300 + 50
		},
		{
			name:           "Tracker nil",
			withTracker:    false,
			setupMock:      func(mt *mockCostTracker) {},
			expectedCost:   0,
			expectedDaily:  0,
			expectedTotalM: 0,
			expectedTotalH: 0,
			expectedTotalO: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mt := &mockCostTracker{}
			tt.setupMock(mt)

			var opts []option
			if tt.withTracker {
				opts = append(opts, WithTracker(mt))
			}
			service := NewService(opts...)

			cost, dailyCost, totalM, totalH, totalO := service.GetStatusData(context.Background())

			assert.Equal(t, tt.expectedCost, cost)
			assert.Equal(t, tt.expectedDaily, dailyCost)
			assert.Equal(t, tt.expectedTotalM, totalM)
			assert.Equal(t, tt.expectedTotalH, totalH)
			assert.Equal(t, tt.expectedTotalO, totalO)

			if tt.withTracker {
				mt.AssertExpectations(t)
			}
		})
	}
}
