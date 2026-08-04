// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"testing"
)

// TestEventTypeStrings verifies that every event type's Type() method returns
// its canonical name. Table-driven over all 18 event types declared in
// types.go, using zero-value instances. This closes the coverage gap on
// UserPromptEvent.Type() (previously the lone 0% Type() method) and locks in
// the type-name contract for dispatch consumers (event bus subscribers, UI
// bridge, turns logger).
func TestEventTypeStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		evt  Event
		want string
	}{
		{"ConfigUpdated", ConfigUpdated{}, "ConfigUpdated"},
		{"TurnStatusEvent", TurnStatusEvent{}, "TurnStatusEvent"},
		{"StatusUpdate", StatusUpdate{}, "StatusUpdate"},
		{"TurnStarted", TurnStarted{}, "TurnStarted"},
		{"InferenceStartedEvent", InferenceStartedEvent{}, "InferenceStartedEvent"},
		{"SummarizationStartedEvent", SummarizationStartedEvent{}, "SummarizationStartedEvent"},
		{"ResponseEvent", ResponseEvent{}, "ResponseEvent"},
		{"ToolCallEvent", ToolCallEvent{}, "ToolCallEvent"},
		{"ToolExecutionStartedEvent", ToolExecutionStartedEvent{}, "ToolExecutionStartedEvent"},
		{"ToolResultEvent", ToolResultEvent{}, "ToolResultEvent"},
		{"UsageMetricsEvent", UsageMetricsEvent{}, "UsageMetricsEvent"},
		{"UserPromptEvent", UserPromptEvent{}, "UserPromptEvent"},
		{"SystemMessageEvent", SystemMessageEvent{}, "SystemMessageEvent"},
		{"TokenLimitReachedEvent", TokenLimitReachedEvent{}, "TokenLimitReachedEvent"},
		{"SummarizationRequired", SummarizationRequired{}, "SummarizationRequired"},
		{"TraceEvent", TraceEvent{}, "TraceEvent"},
		{"RetryWaitingEvent", RetryWaitingEvent{}, "RetryWaitingEvent"},
		{"ToolOutputStreamEvent", ToolOutputStreamEvent{}, "ToolOutputStreamEvent"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.evt.Type(); got != tt.want {
				t.Errorf("%T.Type() = %q; want %q", tt.evt, got, tt.want)
			}
		})
	}
}

// TestSpinnerInfoContract verifies that every event type implementing
// SpinnerInfo() reports ok == true with a non-empty status. The spinner
// coordinator relies on this contract when driving the progress TUI.
func TestSpinnerInfoContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		evt  interface {
			SpinnerInfo() (SpinnerInfo, bool)
		}
	}{
		{"InferenceStartedEvent", InferenceStartedEvent{}},
		{"SummarizationStartedEvent", SummarizationStartedEvent{}},
		{"ToolExecutionStartedEvent", ToolExecutionStartedEvent{}},
		{"RetryWaitingEvent", RetryWaitingEvent{}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info, ok := tt.evt.SpinnerInfo()
			if !ok {
				t.Errorf("%T.SpinnerInfo() ok = false; want true", tt.evt)
			}
			if info.Status == "" {
				t.Errorf("%T.SpinnerInfo() returned empty status", tt.evt)
			}
		})
	}
}
