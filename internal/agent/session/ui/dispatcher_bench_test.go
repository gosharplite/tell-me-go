// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// --- benchRenderer: zero-allocation UIRenderer ---------------------------------

var _ ports.UIRenderer = (*benchRenderer)(nil)

// benchRenderer satisfies ports.UIRenderer with zero stored fields. All
// methods are no-ops except the StartSpinner* family, which return closures
// to mirror real production allocation costs.
type benchRenderer struct{}

func (b *benchRenderer) StartSpinner(_ context.Context) func()                      { return func() {} }
func (b *benchRenderer) StartSpinnerWithStatus(_ context.Context, _ string) func()  { return func() {} }
func (b *benchRenderer) StartSpinnerWithMetrics(_ context.Context, _ string) func() { return func() {} }

func (b *benchRenderer) RenderResponse(_ context.Context, _ *llm.Content, _, _ bool) {}
func (b *benchRenderer) LogTurnStatus(_ context.Context, _ events.TurnStatus)        {}
func (b *benchRenderer) LogUsage(_ context.Context, _ *llm.Metrics, _ string, _ time.Time) {
}
func (b *benchRenderer) LogSystemMessage(_ context.Context, _ string, _ string)      {}
func (b *benchRenderer) RenderHealthReport(_ context.Context, _ *ports.HealthReport) {}
func (b *benchRenderer) LogToolCall(_ context.Context, _ []*llm.FunctionCall, _, _ int, _ bool) {
}
func (b *benchRenderer) LogToolResult(_ context.Context, _ string, _ tools.ToolResult, _ bool) {}
func (b *benchRenderer) SetUseColor(_ bool)                                                    {}
func (b *benchRenderer) SetForceSpinner(_ bool)                                                {}
func (b *benchRenderer) IsTerminalContext() bool                                               { return false }

// --- helper -------------------------------------------------------------------

// newBenchDispatcher constructs an eventDispatcher wired with zero-allocation
// dependencies suitable for benchmarking. Setup cost is excluded from benchmark
// iterations by constructing outside b.N loops.
func newBenchDispatcher() *eventDispatcher {
	renderer := &benchRenderer{}
	logger := &ports.NoOpLogger{}
	sc := newSpinnerCoord(renderer, logger)
	sm := newUIStateMachine(sc)
	return newEventDispatcher(renderer, logger, sm, sc, false, true, false, "")
}

// --- benchmarks ----------------------------------------------------------------

// BenchmarkEventDispatch measures the cost of dispatching each of the 14
// registered event types through the single dispatch chokepoint.
func BenchmarkEventDispatch(b *testing.B) {
	benchmarks := []struct {
		name  string
		event events.Event
	}{
		{"TurnStatusEvent", events.TurnStatusEvent{}},
		{"InferenceStartedEvent", events.InferenceStartedEvent{}},
		{"SummarizationStartedEvent", events.SummarizationStartedEvent{}},
		{"ToolExecutionStartedEvent", events.ToolExecutionStartedEvent{}},
		{"RetryWaitingEvent", events.RetryWaitingEvent{}},
		{"ConsentStartedEvent", events.ConsentStartedEvent{}},
		{"ConsentFinishedEvent", events.ConsentFinishedEvent{}},
		{"ResponseEvent", events.ResponseEvent{}},
		{"UsageMetricsEvent", events.UsageMetricsEvent{Context: context.Background()}},
		{"ToolCallEvent", events.ToolCallEvent{Calls: []*llm.FunctionCall{{}}}},
		{"ToolResultEvent", events.ToolResultEvent{Name: "bench"}},
		{"TurnStarted", events.TurnStarted{}},
		{"SystemMessageEvent", events.SystemMessageEvent{}},
		{"StatusUpdate", events.StatusUpdate{}},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			d := newBenchDispatcher()
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				d.dispatch(ctx, bm.event)
			}
		})
	}
}

// BenchmarkEventDispatchAll dispatches all 14 event types in sequence inside
// the timing loop. State accumulates across events (spinner phase, stopFn
// closure, stateMachine transitions), measuring real dispatch throughput for
// a complete turn lifecycle.
func BenchmarkEventDispatchAll(b *testing.B) {
	d := newBenchDispatcher()
	ctx := context.Background()

	events := []events.Event{
		events.TurnStarted{},
		events.InferenceStartedEvent{},
		events.SystemMessageEvent{},
		events.ToolExecutionStartedEvent{},
		events.ToolCallEvent{Calls: []*llm.FunctionCall{{}}},
		events.ToolResultEvent{Name: "bench"},
		events.ConsentStartedEvent{},
		events.ConsentFinishedEvent{},
		events.SummarizationStartedEvent{},
		events.RetryWaitingEvent{},
		events.UsageMetricsEvent{Context: context.Background()},
		events.TurnStatusEvent{},
		events.ResponseEvent{},
		events.StatusUpdate{},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, e := range events {
			d.dispatch(ctx, e)
		}
	}
}

// BenchmarkEventDispatchUnknown measures the pure dispatch overhead for an
// unregistered event type: reflect.TypeOf + map lookup miss. No handler is
// invoked, so this serves as the floor for the dispatch mechanism itself.
// The 4 unregistered types (ConfigUpdated, TokenLimitReachedEvent,
// SummarizationRequired, TraceEvent) all hit this same no-op path.
func BenchmarkEventDispatchUnknown(b *testing.B) {
	d := newBenchDispatcher()
	ctx := context.Background()
	event := events.ConfigUpdated{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d.dispatch(ctx, event)
	}
}
