// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
)

func TestIsCriticalEvent(t *testing.T) {
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
		{"StatusUpdate", events.StatusUpdate{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isCriticalEvent(tt.event))
		})
	}
}

func TestGetSpinnerInfo(t *testing.T) {
	tests := []struct {
		name     string
		event    events.Event
		expected spinnerInfo
		ok       bool
	}{
		{
			name:  "InferenceStartedEvent with model",
			event: events.InferenceStartedEvent{Model: "gpt-4"},
			expected: spinnerInfo{
				status:         " Thinking [gpt-4]...",
				withMetrics:    false,
				resetRendering: false,
			},
			ok: true,
		},
		{
			name:  "InferenceStartedEvent without model",
			event: events.InferenceStartedEvent{},
			expected: spinnerInfo{
				status:         " Thinking...",
				withMetrics:    false,
				resetRendering: false,
			},
			ok: true,
		},
		{
			name:  "SummarizationStartedEvent",
			event: events.SummarizationStartedEvent{},
			expected: spinnerInfo{
				status:         " Compressing context...",
				withMetrics:    false,
				resetRendering: true,
			},
			ok: true,
		},
		{
			name:  "ToolExecutionStartedEvent single tool",
			event: events.ToolExecutionStartedEvent{ToolNames: []string{"search"}},
			expected: spinnerInfo{
				status:         " Executing [search]...",
				withMetrics:    true,
				resetRendering: true,
			},
			ok: true,
		},
		{
			name:  "ToolExecutionStartedEvent multiple tools",
			event: events.ToolExecutionStartedEvent{ToolNames: []string{"search", "calculator"}},
			expected: spinnerInfo{
				status:         " Executing tools [search, calculator]...",
				withMetrics:    true,
				resetRendering: true,
			},
			ok: true,
		},
		{
			name:  "RetryWaitingEvent",
			event: events.RetryWaitingEvent{Duration: 5 * time.Second},
			expected: spinnerInfo{
				status:         " Retrying in 5s...",
				withMetrics:    false,
				resetRendering: true,
			},
			ok: true,
		},
		{
			name:     "Other event",
			event:    events.ResponseEvent{},
			expected: spinnerInfo{},
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := getSpinnerInfo(tt.event)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.expected, info)
			}
		})
	}
}
