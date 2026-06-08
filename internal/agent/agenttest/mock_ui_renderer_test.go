// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ---------------------------------------------------------------------------
// ResponseRenderer tests
// ---------------------------------------------------------------------------

func TestMockUIRenderer_StartSpinner(t *testing.T) {
	t.Parallel()

	t.Run("nil fn returns no-op stop func", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		stop := m.StartSpinner(ctx)
		if stop == nil {
			t.Fatal("got nil stop func; want non-nil no-op")
		}
		// Must not panic.
		stop()

		snap := m.Snapshot()
		if snap.StartSpinner != 1 {
			t.Errorf("StartSpinner count = %d; want 1", snap.StartSpinner)
		}
	})

	t.Run("custom fn returns fn result", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		called := false
		m := &MockUIRenderer{
			StartSpinnerFn: func(_ context.Context) func() {
				return func() { called = true }
			},
		}

		stop := m.StartSpinner(ctx)
		if stop == nil {
			t.Fatal("got nil stop func; want non-nil")
		}
		stop()
		if !called {
			t.Error("custom stop func was not called")
		}

		snap := m.Snapshot()
		if snap.StartSpinner != 1 {
			t.Errorf("StartSpinner count = %d; want 1", snap.StartSpinner)
		}
	})

	t.Run("snapshot", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		m.StartSpinner(ctx)
		snap := m.Snapshot()

		if snap.StartSpinner != 1 {
			t.Errorf("StartSpinner = %d; want 1", snap.StartSpinner)
		}
		if len(snap.Methods) < 1 || snap.Methods[0] != "StartSpinner" {
			t.Errorf("Methods[0] = %q; want %q", snap.Methods[0], "StartSpinner")
		}
	})
}

func TestMockUIRenderer_StartSpinnerWithStatus(t *testing.T) {
	t.Parallel()

	t.Run("nil fn returns no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		stop := m.StartSpinnerWithStatus(ctx, "loading")
		if stop == nil {
			t.Fatal("got nil stop func; want non-nil no-op")
		}
		stop()

		snap := m.Snapshot()
		if snap.StartSpinnerWithStatus != 1 {
			t.Errorf("StartSpinnerWithStatus count = %d; want 1", snap.StartSpinnerWithStatus)
		}
	})

	t.Run("custom fn receives status", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		var gotStatus string
		called := false
		m := &MockUIRenderer{
			StartSpinnerWithStatusFn: func(_ context.Context, status string) func() {
				gotStatus = status
				return func() { called = true }
			},
		}

		stop := m.StartSpinnerWithStatus(ctx, "loading")
		if gotStatus != "loading" {
			t.Errorf("status = %q; want %q", gotStatus, "loading")
		}
		stop()
		if !called {
			t.Error("custom stop func was not called")
		}

		snap := m.Snapshot()
		if snap.StartSpinnerWithStatus != 1 {
			t.Errorf("StartSpinnerWithStatus count = %d; want 1", snap.StartSpinnerWithStatus)
		}
	})
}

func TestMockUIRenderer_StartSpinnerWithMetrics(t *testing.T) {
	t.Parallel()

	t.Run("nil fn returns no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		stop := m.StartSpinnerWithMetrics(ctx, "processing")
		if stop == nil {
			t.Fatal("got nil stop func; want non-nil no-op")
		}
		stop()

		snap := m.Snapshot()
		if snap.StartSpinnerWithMetrics != 1 {
			t.Errorf("StartSpinnerWithMetrics count = %d; want 1", snap.StartSpinnerWithMetrics)
		}
	})

	t.Run("custom fn receives status", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		var gotStatus string
		called := false
		m := &MockUIRenderer{
			StartSpinnerWithMetricsFn: func(_ context.Context, status string) func() {
				gotStatus = status
				return func() { called = true }
			},
		}

		stop := m.StartSpinnerWithMetrics(ctx, "processing")
		if gotStatus != "processing" {
			t.Errorf("status = %q; want %q", gotStatus, "processing")
		}
		stop()
		if !called {
			t.Error("custom stop func was not called")
		}

		snap := m.Snapshot()
		if snap.StartSpinnerWithMetrics != 1 {
			t.Errorf("StartSpinnerWithMetrics count = %d; want 1", snap.StartSpinnerWithMetrics)
		}
	})
}

func TestMockUIRenderer_RenderResponse(t *testing.T) {
	t.Parallel()

	t.Run("nil fn is no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		content := &llm.Content{Role: "assistant"}
		m := &MockUIRenderer{}

		// Must not panic.
		m.RenderResponse(ctx, content, true, false)

		snap := m.Snapshot()
		if snap.RenderResponse != 1 {
			t.Errorf("RenderResponse count = %d; want 1", snap.RenderResponse)
		}
	})

	t.Run("custom fn receives args", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		content := &llm.Content{Role: "assistant"}
		var gotRole string
		var gotShowThoughts, gotRawOutput bool
		m := &MockUIRenderer{
			RenderResponseFn: func(_ context.Context, c *llm.Content, showThoughts, rawOutput bool) {
				gotRole = c.Role
				gotShowThoughts = showThoughts
				gotRawOutput = rawOutput
			},
		}

		m.RenderResponse(ctx, content, true, false)

		if gotRole != "assistant" {
			t.Errorf("role = %q; want %q", gotRole, "assistant")
		}
		if !gotShowThoughts {
			t.Error("showThoughts = false; want true")
		}
		if gotRawOutput {
			t.Error("rawOutput = true; want false")
		}

		snap := m.Snapshot()
		if snap.RenderResponse != 1 {
			t.Errorf("RenderResponse count = %d; want 1", snap.RenderResponse)
		}
	})
}

// ---------------------------------------------------------------------------
// StatusLogger tests
// ---------------------------------------------------------------------------

func TestMockUIRenderer_LogTurnStatus(t *testing.T) {
	t.Parallel()

	t.Run("nil fn no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		// Must not panic.
		m.LogTurnStatus(ctx, events.TurnStatus{SessionTurns: 1})

		snap := m.Snapshot()
		if snap.LogTurnStatus != 1 {
			t.Errorf("LogTurnStatus count = %d; want 1", snap.LogTurnStatus)
		}
	})

	t.Run("custom fn receives status", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		var gotStatus events.TurnStatus
		m := &MockUIRenderer{
			LogTurnStatusFn: func(_ context.Context, status events.TurnStatus) {
				gotStatus = status
			},
		}

		m.LogTurnStatus(ctx, events.TurnStatus{SessionTurns: 1})

		if gotStatus.SessionTurns != 1 {
			t.Errorf("SessionTurns = %d; want 1", gotStatus.SessionTurns)
		}

		snap := m.Snapshot()
		if snap.LogTurnStatus != 1 {
			t.Errorf("LogTurnStatus count = %d; want 1", snap.LogTurnStatus)
		}
	})
}

func TestMockUIRenderer_LogSystemMessage(t *testing.T) {
	t.Parallel()

	t.Run("nil fn no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		// Must not panic.
		m.LogSystemMessage(ctx, "test", "info")

		snap := m.Snapshot()
		if snap.LogSystemMessage != 1 {
			t.Errorf("LogSystemMessage count = %d; want 1", snap.LogSystemMessage)
		}
	})

	t.Run("custom fn receives msg and level", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		var gotMsg, gotLevel string
		m := &MockUIRenderer{
			LogSystemMessageFn: func(_ context.Context, msg string, level string) {
				gotMsg = msg
				gotLevel = level
			},
		}

		m.LogSystemMessage(ctx, "test", "info")

		if gotMsg != "test" {
			t.Errorf("msg = %q; want %q", gotMsg, "test")
		}
		if gotLevel != "info" {
			t.Errorf("level = %q; want %q", gotLevel, "info")
		}

		snap := m.Snapshot()
		if snap.LogSystemMessage != 1 {
			t.Errorf("LogSystemMessage count = %d; want 1", snap.LogSystemMessage)
		}
	})
}

func TestMockUIRenderer_RenderHealthReport(t *testing.T) {
	t.Parallel()

	t.Run("nil fn no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		// Must not panic.
		m.RenderHealthReport(ctx, &ports.HealthReport{OverallStatus: ports.StatusHealthy})

		snap := m.Snapshot()
		if snap.RenderHealthReport != 1 {
			t.Errorf("RenderHealthReport count = %d; want 1", snap.RenderHealthReport)
		}
	})

	t.Run("custom fn receives report", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		var gotOverall ports.HealthStatus
		m := &MockUIRenderer{
			RenderHealthReportFn: func(_ context.Context, report *ports.HealthReport) {
				gotOverall = report.OverallStatus
			},
		}

		m.RenderHealthReport(ctx, &ports.HealthReport{OverallStatus: ports.StatusHealthy})

		if gotOverall != ports.StatusHealthy {
			t.Errorf("OverallStatus = %q; want %q", gotOverall, ports.StatusHealthy)
		}

		snap := m.Snapshot()
		if snap.RenderHealthReport != 1 {
			t.Errorf("RenderHealthReport count = %d; want 1", snap.RenderHealthReport)
		}
	})
}

// ---------------------------------------------------------------------------
// UsageLogger test
// ---------------------------------------------------------------------------

func TestMockUIRenderer_LogUsage(t *testing.T) {
	t.Parallel()

	t.Run("nil fn no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		// Must not panic.
		m.LogUsage(ctx, &llm.Metrics{PromptTokens: 10}, "/tmp/log", time.Now())

		snap := m.Snapshot()
		if snap.LogUsage != 1 {
			t.Errorf("LogUsage count = %d; want 1", snap.LogUsage)
		}
	})

	t.Run("custom fn receives args", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		var gotTokens int32
		var gotLogFile string
		m := &MockUIRenderer{
			LogUsageFn: func(_ context.Context, metrics *llm.Metrics, logFile string, _ time.Time) {
				gotTokens = metrics.PromptTokens
				gotLogFile = logFile
			},
		}

		m.LogUsage(ctx, &llm.Metrics{PromptTokens: 10}, "/tmp/log", time.Now())

		if gotTokens != 10 {
			t.Errorf("PromptTokens = %d; want 10", gotTokens)
		}
		if gotLogFile != "/tmp/log" {
			t.Errorf("logFile = %q; want %q", gotLogFile, "/tmp/log")
		}

		snap := m.Snapshot()
		if snap.LogUsage != 1 {
			t.Errorf("LogUsage count = %d; want 1", snap.LogUsage)
		}
	})
}

// ---------------------------------------------------------------------------
// ToolLogger tests
// ---------------------------------------------------------------------------

func TestMockUIRenderer_LogToolCall(t *testing.T) {
	t.Parallel()

	t.Run("nil fn no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		calls := []*llm.FunctionCall{{Name: "test"}}
		m := &MockUIRenderer{}

		// Must not panic.
		m.LogToolCall(ctx, calls, 1, 5, true)

		snap := m.Snapshot()
		if snap.LogToolCall != 1 {
			t.Errorf("LogToolCall count = %d; want 1", snap.LogToolCall)
		}
	})

	t.Run("custom fn receives args", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		calls := []*llm.FunctionCall{{Name: "test"}}
		var gotName string
		var gotTurn, gotMaxTurns int
		var gotShowTools bool
		m := &MockUIRenderer{
			LogToolCallFn: func(_ context.Context, c []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
				if len(c) > 0 {
					gotName = c[0].Name
				}
				gotTurn = turn
				gotMaxTurns = maxTurns
				gotShowTools = showTools
			},
		}

		m.LogToolCall(ctx, calls, 1, 5, true)

		if gotName != "test" {
			t.Errorf("name = %q; want %q", gotName, "test")
		}
		if gotTurn != 1 {
			t.Errorf("turn = %d; want 1", gotTurn)
		}
		if gotMaxTurns != 5 {
			t.Errorf("maxTurns = %d; want 5", gotMaxTurns)
		}
		if !gotShowTools {
			t.Error("showTools = false; want true")
		}

		snap := m.Snapshot()
		if snap.LogToolCall != 1 {
			t.Errorf("LogToolCall count = %d; want 1", snap.LogToolCall)
		}
	})
}

func TestMockUIRenderer_LogToolResult(t *testing.T) {
	t.Parallel()

	t.Run("nil fn no-op", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		m := &MockUIRenderer{}

		// Must not panic.
		m.LogToolResult(ctx, "test-tool", tools.ToolResult{Text: "done"}, false)

		snap := m.Snapshot()
		if snap.LogToolResult != 1 {
			t.Errorf("LogToolResult count = %d; want 1", snap.LogToolResult)
		}
	})

	t.Run("custom fn receives args", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		var gotName, gotText string
		var gotShowTools bool
		m := &MockUIRenderer{
			LogToolResultFn: func(_ context.Context, name string, result tools.ToolResult, showTools bool) {
				gotName = name
				gotText = result.Text
				gotShowTools = showTools
			},
		}

		m.LogToolResult(ctx, "test-tool", tools.ToolResult{Text: "done"}, false)

		if gotName != "test-tool" {
			t.Errorf("name = %q; want %q", gotName, "test-tool")
		}
		if gotText != "done" {
			t.Errorf("text = %q; want %q", gotText, "done")
		}
		if gotShowTools {
			t.Error("showTools = true; want false")
		}

		snap := m.Snapshot()
		if snap.LogToolResult != 1 {
			t.Errorf("LogToolResult count = %d; want 1", snap.LogToolResult)
		}
	})
}

// ---------------------------------------------------------------------------
// RendererConfigurator tests
// ---------------------------------------------------------------------------

func TestMockUIRenderer_SetUseColor(t *testing.T) {
	t.Parallel()

	t.Run("nil fn no-op", func(t *testing.T) {
		t.Parallel()

		m := &MockUIRenderer{}

		// Must not panic.
		m.SetUseColor(true)

		snap := m.Snapshot()
		if snap.SetUseColor != 1 {
			t.Errorf("SetUseColor count = %d; want 1", snap.SetUseColor)
		}
	})

	t.Run("custom fn receives use", func(t *testing.T) {
		t.Parallel()

		var gotUse bool
		m := &MockUIRenderer{
			SetUseColorFn: func(use bool) {
				gotUse = use
			},
		}

		m.SetUseColor(true)

		if !gotUse {
			t.Error("use = false; want true")
		}

		snap := m.Snapshot()
		if snap.SetUseColor != 1 {
			t.Errorf("SetUseColor count = %d; want 1", snap.SetUseColor)
		}
	})
}

func TestMockUIRenderer_SetForceSpinner(t *testing.T) {
	t.Parallel()

	t.Run("nil fn no-op", func(t *testing.T) {
		t.Parallel()

		m := &MockUIRenderer{}

		// Must not panic.
		m.SetForceSpinner(false)

		snap := m.Snapshot()
		if snap.SetForceSpinner != 1 {
			t.Errorf("SetForceSpinner count = %d; want 1", snap.SetForceSpinner)
		}
	})

	t.Run("custom fn receives force", func(t *testing.T) {
		t.Parallel()

		var gotForce bool
		m := &MockUIRenderer{
			SetForceSpinnerFn: func(force bool) {
				gotForce = force
			},
		}

		m.SetForceSpinner(false)

		if gotForce {
			t.Error("force = true; want false")
		}

		snap := m.Snapshot()
		if snap.SetForceSpinner != 1 {
			t.Errorf("SetForceSpinner count = %d; want 1", snap.SetForceSpinner)
		}
	})
}

func TestMockUIRenderer_IsTerminalContext(t *testing.T) {
	t.Parallel()

	t.Run("nil fn returns false", func(t *testing.T) {
		t.Parallel()

		m := &MockUIRenderer{}

		got := m.IsTerminalContext()
		if got {
			t.Error("got true; want false (nil fn default)")
		}

		snap := m.Snapshot()
		if snap.IsTerminalContext != 1 {
			t.Errorf("IsTerminalContext count = %d; want 1", snap.IsTerminalContext)
		}
	})

	t.Run("custom fn returns true", func(t *testing.T) {
		t.Parallel()

		m := &MockUIRenderer{
			IsTerminalContextFn: func() bool {
				return true
			},
		}

		got := m.IsTerminalContext()
		if !got {
			t.Error("got false; want true")
		}

		snap := m.Snapshot()
		if snap.IsTerminalContext != 1 {
			t.Errorf("IsTerminalContext count = %d; want 1", snap.IsTerminalContext)
		}
	})
}

// ---------------------------------------------------------------------------
// Snapshot integration test
// ---------------------------------------------------------------------------

func TestMockUIRenderer_Snapshot_AllMethods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &MockUIRenderer{}

	// Call one method from each sub-interface.
	m.SetUseColor(true)                                     // RendererConfigurator
	m.StartSpinner(ctx)                                     // ResponseRenderer
	m.LogTurnStatus(ctx, events.TurnStatus{})               // StatusLogger
	m.LogUsage(ctx, &llm.Metrics{}, "/tmp/log", time.Now()) // UsageLogger
	m.LogToolCall(ctx, nil, 0, 0, false)                    // ToolLogger

	snap := m.Snapshot()

	// Called counters must be 1.
	if snap.SetUseColor != 1 {
		t.Errorf("SetUseColor = %d; want 1", snap.SetUseColor)
	}
	if snap.StartSpinner != 1 {
		t.Errorf("StartSpinner = %d; want 1", snap.StartSpinner)
	}
	if snap.LogTurnStatus != 1 {
		t.Errorf("LogTurnStatus = %d; want 1", snap.LogTurnStatus)
	}
	if snap.LogUsage != 1 {
		t.Errorf("LogUsage = %d; want 1", snap.LogUsage)
	}
	if snap.LogToolCall != 1 {
		t.Errorf("LogToolCall = %d; want 1", snap.LogToolCall)
	}

	// Uncalled counters must be 0.
	if snap.StartSpinnerWithStatus != 0 {
		t.Errorf("StartSpinnerWithStatus = %d; want 0", snap.StartSpinnerWithStatus)
	}
	if snap.StartSpinnerWithMetrics != 0 {
		t.Errorf("StartSpinnerWithMetrics = %d; want 0", snap.StartSpinnerWithMetrics)
	}
	if snap.RenderResponse != 0 {
		t.Errorf("RenderResponse = %d; want 0", snap.RenderResponse)
	}
	if snap.LogSystemMessage != 0 {
		t.Errorf("LogSystemMessage = %d; want 0", snap.LogSystemMessage)
	}
	if snap.RenderHealthReport != 0 {
		t.Errorf("RenderHealthReport = %d; want 0", snap.RenderHealthReport)
	}
	if snap.LogToolResult != 0 {
		t.Errorf("LogToolResult = %d; want 0", snap.LogToolResult)
	}
	if snap.SetForceSpinner != 0 {
		t.Errorf("SetForceSpinner = %d; want 0", snap.SetForceSpinner)
	}
	if snap.IsTerminalContext != 0 {
		t.Errorf("IsTerminalContext = %d; want 0", snap.IsTerminalContext)
	}

	// Methods slice must have correct order and length.
	wantMethods := []string{"SetUseColor", "StartSpinner", "LogTurnStatus", "LogUsage", "LogToolCall"}
	if len(snap.Methods) != len(wantMethods) {
		t.Fatalf("len(Methods) = %d; want %d", len(snap.Methods), len(wantMethods))
	}
	for i, want := range wantMethods {
		if snap.Methods[i] != want {
			t.Errorf("Methods[%d] = %q; want %q", i, snap.Methods[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrency test
// ---------------------------------------------------------------------------

// Run: go test -race -run TestMockUIRenderer_Concurrency
func TestMockUIRenderer_Concurrency(t *testing.T) {
	t.Parallel()

	m := &MockUIRenderer{}
	var wg sync.WaitGroup
	n := 10

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			m.LogTurnStatus(context.Background(), events.TurnStatus{})
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	if snap.LogTurnStatus != n {
		t.Errorf("LogTurnStatus = %d; want %d", snap.LogTurnStatus, n)
	}
}
