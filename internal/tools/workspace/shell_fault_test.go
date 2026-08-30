// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

// newFaultShellTool builds the two-layer fake stack (issue #1460, ADR-074):
// fake runner → real processExecutor → real shellTool. The shell's own
// translation/authorization/feedback layers run for real on top.
func newFaultShellTool(t *testing.T, fake *toolstest.FakeProcessRunner) (*shellTool, *eventstest.TestEventBus) {
	t.Helper()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	bus := &eventstest.TestEventBus{}
	tool := newshellTool(sm, bus, &toolstest.MockCommandValidator{}, &posixTranslator{}, &posixShellWrapper{}, persistencetest.NewPlainOSFileSystem(), fake)
	return tool, bus
}

// TestShellFaultExecuteCommand drives ExecuteCommand through the fake runner:
// the start-error ToolResult, the exit-code formatting, and the deadline
// formatting with a partial-output section — the branches the #1431 batch
// cataloged as fault-injection-required in shell.go's execution paths.
func TestShellFaultExecuteCommand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("start error becomes an error ToolResult", func(t *testing.T) {
		t.Parallel()
		fake := &toolstest.FakeProcessRunner{
			StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
				return nil, errors.New("spawn fail")
			},
		}
		tool, _ := newFaultShellTool(t, fake)

		res, err := tool.ExecuteCommand(ctx, map[string]interface{}{"command": "s0", "reason": "t"}, nil)
		if err != nil {
			t.Fatalf("ExecuteCommand returned error: %v", err)
		}
		if res.Error == nil || !strings.Contains(res.Error.Error(), "failed to start: spawn fail") {
			t.Errorf("res.Error = %v; want the wrapped start error", res.Error)
		}
	})

	t.Run("exit status 2 formats the exit code", func(t *testing.T) {
		t.Parallel()
		fake := &toolstest.FakeProcessRunner{
			StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
				return fakeHandle("out\n", "", &tools.ExitError{Code: 2}), nil
			},
		}
		tool, _ := newFaultShellTool(t, fake)

		res, err := tool.ExecuteCommand(ctx, map[string]interface{}{"command": "s0", "reason": "t"}, nil)
		if err != nil {
			t.Fatalf("ExecuteCommand returned error: %v", err)
		}
		if res.Error != nil {
			t.Fatalf("res.Error = %v; want nil (exit-status failure is a result, not an error)", res.Error)
		}
		if !strings.Contains(res.Text, "Exit Code: 2") {
			t.Errorf("res.Text = %q; want 'Exit Code: 2'", res.Text)
		}
		if !strings.Contains(res.Text, "out") {
			t.Errorf("res.Text = %q; want canned output", res.Text)
		}
	})

	t.Run("deadline exceeded formats the timeout result with partial output", func(t *testing.T) {
		t.Parallel()
		fake := &toolstest.FakeProcessRunner{
			StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
				return fakeHandle("partial\n", "", context.DeadlineExceeded), nil
			},
		}
		tool, _ := newFaultShellTool(t, fake)

		res, err := tool.ExecuteCommand(ctx, map[string]interface{}{"command": "s0", "reason": "t"}, nil)
		if err != nil {
			t.Fatalf("ExecuteCommand returned error: %v", err)
		}
		if !errors.Is(res.Error, context.DeadlineExceeded) {
			t.Errorf("res.Error = %v; want context.DeadlineExceeded", res.Error)
		}
		if !strings.Contains(res.Text, "timed out after") {
			t.Errorf("res.Text = %q; want the timeout wording", res.Text)
		}
		if !strings.Contains(res.Text, "Partial output before timeout:\npartial") {
			t.Errorf("res.Text = %q; want the partial-output section", res.Text)
		}
	})
}

// TestShellFaultPipeCommands drives PipeCommands through the fake runner:
// pipeline wait errors format as "Pipeline result. Exit Code: N".
func TestShellFaultPipeCommands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fake := &toolstest.FakeProcessRunner{}
	fake.StartFunc = func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
		switch len(fake.Calls) { // start order 0..n-1 (ADR-074 D4 contract 1)
		case 0:
			return fakeHandle("p0\n", "", nil), nil
		default:
			return fakeHandle("p1\n", "", &tools.ExitError{Code: 5}), nil
		}
	}
	tool, _ := newFaultShellTool(t, fake)

	res, err := tool.PipeCommands(ctx, map[string]interface{}{
		"commands": []string{"s0", "s1"},
		"reason":   "t",
	}, nil)
	if err != nil {
		t.Fatalf("PipeCommands returned error: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("res.Error = %v; want nil (exit-status failure formats as a result)", res.Error)
	}
	if !strings.Contains(res.Text, "Pipeline result. Exit Code: 5") {
		t.Errorf("res.Text = %q; want 'Pipeline result. Exit Code: 5'", res.Text)
	}
	// Pipeline stdout capture reads the LAST stage's reader only (stage0's
	// output would flow through stage1's stdin in a real pipe — the fake
	// ignores Stdin).
	if !strings.Contains(res.Text, "p1") {
		t.Errorf("res.Text = %q; want the last stage's stdout", res.Text)
	}

	t.Run("start error becomes an error ToolResult", func(t *testing.T) {
		t.Parallel()
		fake := &toolstest.FakeProcessRunner{
			StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
				return nil, errors.New("spawn fail")
			},
		}
		tool, _ := newFaultShellTool(t, fake)

		res, err := tool.PipeCommands(ctx, map[string]interface{}{
			"commands": []string{"s0", "s1"},
			"reason":   "t",
		}, nil)
		if err != nil {
			t.Fatalf("PipeCommands returned error: %v", err)
		}
		if res.Error == nil || !strings.Contains(res.Error.Error(), "pipeline failed to start") {
			t.Errorf("res.Error = %v; want the wrapped pipeline start error", res.Error)
		}
	})

	t.Run("deadline exceeded formats the timeout result", func(t *testing.T) {
		t.Parallel()
		fake := &toolstest.FakeProcessRunner{
			StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
				return fakeHandle("partial\n", "", context.DeadlineExceeded), nil
			},
		}
		tool, _ := newFaultShellTool(t, fake)

		res, err := tool.PipeCommands(ctx, map[string]interface{}{
			"commands": []string{"s0", "s1"},
			"reason":   "t",
		}, nil)
		if err != nil {
			t.Fatalf("PipeCommands returned error: %v", err)
		}
		if !errors.Is(res.Error, context.DeadlineExceeded) {
			t.Errorf("res.Error = %v; want context.DeadlineExceeded", res.Error)
		}
		if !strings.Contains(res.Text, "timed out after") {
			t.Errorf("res.Text = %q; want the timeout wording", res.Text)
		}
		if !strings.Contains(res.Text, "Partial output before timeout:\npartial") {
			t.Errorf("res.Text = %q; want the partial-output section", res.Text)
		}
	})
}

// TestShellFaultRunWithFeedbackEvents pins runWithFeedback's three
// output-stream publications: header, separator, execution, separator.
func TestShellFaultRunWithFeedbackEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fake := &toolstest.FakeProcessRunner{
		StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
			return fakeHandle("line\n", "", nil), nil
		},
	}
	tool, bus := newFaultShellTool(t, fake)

	if _, err := tool.ExecuteCommand(ctx, map[string]interface{}{"command": "s0", "reason": "t"}, nil); err != nil {
		t.Fatalf("ExecuteCommand returned error: %v", err)
	}

	streamEvents := bus.FilterEvents(reflect.TypeOf(events.ToolOutputStreamEvent{}))
	if len(streamEvents) < 3 {
		t.Fatalf("ToolOutputStreamEvent count = %d; want at least 3 (header, separator, separator)", len(streamEvents))
	}
	first := streamEvents[0].(events.ToolOutputStreamEvent)
	if !strings.Contains(first.Message, "Executing") || !strings.Contains(first.Message, "(Output shown below)") {
		t.Errorf("first event = %q; want the 'Executing... (Output shown below)' header", first.Message)
	}
	sep := strings.Repeat("-", 60)
	second := streamEvents[1].(events.ToolOutputStreamEvent)
	if second.Message != sep {
		t.Errorf("second event = %q; want the separator", second.Message)
	}
	last := streamEvents[len(streamEvents)-1].(events.ToolOutputStreamEvent)
	if last.Message != sep {
		t.Errorf("last event = %q; want the closing separator", last.Message)
	}
}
