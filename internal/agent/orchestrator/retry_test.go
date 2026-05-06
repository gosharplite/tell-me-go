// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestDefaultRetryPolicy_ShouldRetry(t *testing.T) {
	policy := &DefaultRetryPolicy{
		MaxRetries:       15, // Increase to allow ceiling test
		Backoff:          2 * time.Second,
		RateLimitBackoff: 10 * time.Second,
	}
	mc := &agenttest.MockClock{}

	tests := []struct {
		name             string
		err              error
		attempt          int
		hasSeenRateLimit bool
		wantDelay        time.Duration
		wantRetry        bool
	}{
		{
			name:      "Max retries reached",
			err:       llm.ErrTransient,
			attempt:   15,
			wantRetry: false,
		},
		{
			name:      "Fatal error",
			err:       llm.ErrTerminal,
			attempt:   0,
			wantRetry: false,
		},
		{
			name:             "Transient error - first attempt",
			err:              llm.ErrTransient,
			attempt:          0,
			hasSeenRateLimit: false,
			wantDelay:        2 * time.Second,
			wantRetry:        true,
		},
		{
			name:             "Transient error - second attempt (backoff)",
			err:              llm.ErrTransient,
			attempt:          1,
			hasSeenRateLimit: false,
			wantDelay:        4 * time.Second,
			wantRetry:        true,
		},
		{
			name:             "Transient error - third attempt (backoff)",
			err:              llm.ErrTransient,
			attempt:          2,
			hasSeenRateLimit: false,
			wantDelay:        8 * time.Second,
			wantRetry:        true,
		},
		{
			name:             "Rate limit backoff - first attempt",
			err:              llm.ErrRateLimit,
			attempt:          0,
			hasSeenRateLimit: true,
			wantDelay:        10 * time.Second,
			wantRetry:        true,
		},
		{
			name:             "Rate limit backoff - second attempt",
			err:              llm.ErrTransient, // Current error transient, but seen rate limit before
			attempt:          1,
			hasSeenRateLimit: true,
			wantDelay:        20 * time.Second,
			wantRetry:        true,
		},
		{
			name:             "Max delay ceiling",
			err:              llm.ErrTransient,
			attempt:          10, // 2^10 * 2s is way over 2m
			hasSeenRateLimit: false,
			wantDelay:        2 * time.Minute,
			wantRetry:        true,
		},
		{
			name:      "Non-transient error",
			err:       errors.New("unknown error"),
			attempt:   0,
			wantRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay, retry := policy.ShouldRetry(mc, tt.err, tt.attempt, tt.hasSeenRateLimit)

			assert.Equal(t, tt.wantRetry, retry)
			if tt.wantRetry {
				assert.Equal(t, tt.wantDelay, delay)
			}
		})
	}
}

func TestDefaultRetryPolicy_OverflowSafety(t *testing.T) {
	policy := &DefaultRetryPolicy{
		MaxRetries:       100,
		Backoff:          2 * time.Second,
		RateLimitBackoff: 10 * time.Second,
	}
	mc := &agenttest.MockClock{}
	const maxDelay = 2 * time.Minute

	tests := []struct {
		name    string
		attempt int
	}{
		{"Extreme attempt 31", 31},
		{"Extreme attempt 63", 63},
		{"Extreme attempt 99", 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay, retry := policy.ShouldRetry(mc, llm.ErrTransient, tt.attempt, false)
			assert.True(t, retry)
			assert.LessOrEqual(t, delay, maxDelay)
			assert.GreaterOrEqual(t, delay, time.Duration(0))
		})
	}
}

func TestCalculateBackoff(t *testing.T) {
	const maxDelay = 2 * time.Minute

	tests := []struct {
		name      string
		base      time.Duration
		attempt   int
		wantDelay time.Duration
	}{
		{
			name:      "zero attempt returns base",
			base:      2 * time.Second,
			attempt:   0,
			wantDelay: 2 * time.Second,
		},
		{
			name:      "single doubling",
			base:      2 * time.Second,
			attempt:   1,
			wantDelay: 4 * time.Second,
		},
		{
			name:      "double doubling",
			base:      2 * time.Second,
			attempt:   2,
			wantDelay: 8 * time.Second,
		},
		{
			name:      "triple doubling",
			base:      2 * time.Second,
			attempt:   3,
			wantDelay: 16 * time.Second,
		},
		{
			name:      "max delay ceiling hit via large attempt",
			base:      2 * time.Second,
			attempt:   10, // 2^10 * 2s = 2048s > 2m
			wantDelay: maxDelay,
		},
		{
			name:      "base exceeds maxDelay from start",
			base:      5 * time.Minute,
			attempt:   0,
			wantDelay: maxDelay,
		},
		{
			name:      "base exceeds maxDelay with attempt > 0",
			base:      5 * time.Minute,
			attempt:   3,
			wantDelay: maxDelay,
		},
		{
			name:      "overflow safety — extreme attempt 63",
			base:      2 * time.Second,
			attempt:   63,
			wantDelay: maxDelay,
		},
		{
			name:      "rate-limit base single doubling",
			base:      10 * time.Second,
			attempt:   1,
			wantDelay: 20 * time.Second,
		},
		{
			name:      "rate-limit base hits ceiling",
			base:      10 * time.Second,
			attempt:   10, // 10s * 2^10 = 10240s > 2m
			wantDelay: maxDelay,
		},
		{
			name:      "zero base duration",
			base:      0,
			attempt:   5,
			wantDelay: 0, // 0 doubled stays 0, never hits maxDelay cap
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateBackoff(tt.base, tt.attempt)
			if got != tt.wantDelay {
				t.Errorf("calculateBackoff(%v, %d) = %v; want %v",
					tt.base, tt.attempt, got, tt.wantDelay)
			}
		})
	}
}
