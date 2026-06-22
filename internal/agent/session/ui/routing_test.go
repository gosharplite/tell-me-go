// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
)

func TestIsCriticalEvent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		event    events.Event
		expected bool
	}{
		{"ResponseEvent", events.ResponseEvent{}, true},
		{"SystemMessageEvent", events.SystemMessageEvent{}, true},
		{"ConsentStartedEvent", events.ConsentStartedEvent{}, true},
		{"ConsentFinishedEvent", events.ConsentFinishedEvent{}, true},
		{"TurnStarted", events.TurnStarted{}, true},
		{"TurnStatusEvent", events.TurnStatusEvent{}, true},
		{"ToolCallEvent", events.ToolCallEvent{}, true},
		{"ToolResultEvent", events.ToolResultEvent{}, true},
		{"UsageMetricsEvent", events.UsageMetricsEvent{}, true},
		{"InferenceStartedEvent", events.InferenceStartedEvent{}, false},
		{"SummarizationStartedEvent", events.SummarizationStartedEvent{}, false},
		{"ToolExecutionStartedEvent", events.ToolExecutionStartedEvent{}, false},
		{"RetryWaitingEvent", events.RetryWaitingEvent{}, false},
		{"StatusUpdate", events.StatusUpdate{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isCriticalEvent(tt.event))
		})
	}
}

func TestGetSpinnerInfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		event    events.Event
		expected events.SpinnerInfo
		ok       bool
	}{
		{
			name:  "InferenceStartedEvent with model",
			event: events.InferenceStartedEvent{Model: "gpt-4"},
			expected: events.SpinnerInfo{
				Status:         " Thinking [gpt-4]...",
				WithMetrics:    false,
				ResetRendering: false,
			},
			ok: true,
		},
		{
			name:  "InferenceStartedEvent without model",
			event: events.InferenceStartedEvent{},
			expected: events.SpinnerInfo{
				Status:         " Thinking...",
				WithMetrics:    false,
				ResetRendering: false,
			},
			ok: true,
		},
		{
			name:  "SummarizationStartedEvent",
			event: events.SummarizationStartedEvent{},
			expected: events.SpinnerInfo{
				Status:         " Compressing context...",
				WithMetrics:    false,
				ResetRendering: true,
			},
			ok: true,
		},
		{
			name:  "ToolExecutionStartedEvent single tool",
			event: events.ToolExecutionStartedEvent{ToolNames: []string{"search"}},
			expected: events.SpinnerInfo{
				Status:         " Executing [search]...",
				WithMetrics:    true,
				ResetRendering: true,
			},
			ok: true,
		},
		{
			name:  "ToolExecutionStartedEvent multiple tools",
			event: events.ToolExecutionStartedEvent{ToolNames: []string{"search", "calculator"}},
			expected: events.SpinnerInfo{
				Status:         " Executing tools [search, calculator]...",
				WithMetrics:    true,
				ResetRendering: true,
			},
			ok: true,
		},
		{
			name:  "RetryWaitingEvent",
			event: events.RetryWaitingEvent{Duration: 5 * time.Second},
			expected: events.SpinnerInfo{
				Status:         " Retrying in 5s...",
				WithMetrics:    false,
				ResetRendering: true,
			},
			ok: true,
		},
		{
			name:     "Other event",
			event:    events.ResponseEvent{},
			expected: events.SpinnerInfo{},
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info, ok := getSpinnerInfo(tt.event)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.expected, info)
			}
		})
	}
}
