// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// authDecorator handles security authorization.
type authDecorator struct {
	next ToolExecutor
	auth ToolAuthService
}

func newAuthDecorator(next ToolExecutor, auth ToolAuthService) ToolExecutor {
	return &authDecorator{next: next, auth: auth}
}

func (d *authDecorator) Execute(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall, hb chan<- struct{}) (tools.ToolResult, error) {
	if err := d.auth.Authorize(ctx, tool, call); err != nil {
		return tools.ToolResult{Text: err.Error(), Error: err}, nil
	}

	return d.next.Execute(ctx, tool, call, hb)
}

// safetyDecorator handles timeouts, panics, and zombie detection.
type safetyDecorator struct {
	next               ToolExecutor
	registry           tools.Registry
	logger             ports.Logger
	events             events.EventBus
	zombie             *tools.ZombieTool
	toolTimeout        time.Duration
	longRunningTimeout time.Duration
	zombieTimeout      time.Duration
}

func newSafetyDecorator(next ToolExecutor, registry tools.Registry, logger ports.Logger, bus events.EventBus, zombie *tools.ZombieTool, toolTimeout, longRunningTimeout, zombieTimeout time.Duration) ToolExecutor {
	return &safetyDecorator{
		next:               next,
		registry:           registry,
		logger:             logger,
		events:             bus,
		zombie:             zombie,
		toolTimeout:        toolTimeout,
		longRunningTimeout: longRunningTimeout,
		zombieTimeout:      zombieTimeout,
	}
}

func (d *safetyDecorator) Execute(parentCtx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall, heartbeat chan<- struct{}) (result tools.ToolResult, err error) {
	opts := d.registry.GetOptions(call.Name)
	activeTimeout := d.resolveTimeout(call, opts)

	ctx, cancel := context.WithTimeout(parentCtx, activeTimeout)
	defer cancel()

	outCh := make(chan tools.ToolOutput, 1)
	hbCh := make(chan struct{}, 1)

	// Liveness check monitoring
	monitorCtx, monitorCancel := context.WithCancel(ctx)
	defer monitorCancel()

	go d.monitorLiveness(monitorCtx, call.Name, opts, hbCh, heartbeat, cancel)

	go d.executeToolSafe(ctx, tool, call, hbCh, outCh)

	select {
	case <-ctx.Done():
		return d.handleTimeout(parentCtx, ctx.Err(), call.Name, activeTimeout, outCh), nil

	case out := <-outCh:
		return out.Result, out.Err
	}
}

func (d *safetyDecorator) executeToolSafe(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall, hbCh chan<- struct{}, outCh chan<- tools.ToolOutput) {
	// CRITICAL: This recover block protects the isolated tool execution thread.
	// It catches panics originating inside the actual tool implementation (e.g., nil pointer dereferences
	// in a third-party SDK) and safely converts them into tool execution errors.
	// Do NOT remove this, as the Dispatcher's main recover block cannot catch panics in this detached goroutine.
	defer func() {
		if r := recover(); r != nil {
			outCh <- tools.ToolOutput{
				Result: d.handlePanic(ctx, r, call.Name),
			}
		}
		close(hbCh) // Signal monitor that tool finished
	}()
	res, execErr := d.next.Execute(ctx, tool, call, hbCh)
	outCh <- tools.ToolOutput{Result: res, Err: execErr}
}

func (d *safetyDecorator) resolveTimeout(call *llm.FunctionCall, opts tools.ToolOptions) time.Duration {
	activeTimeout := d.toolTimeout
	if opts.LongRunning {
		activeTimeout = d.longRunningTimeout
	}

	// NEW: Allow the payload to dynamically override the timeout
	if tVal, ok := call.Args["timeout"]; ok {
		var reqSeconds float64
		switch v := tVal.(type) {
		case float64:
			reqSeconds = v
		case int:
			reqSeconds = float64(v)
		case int64:
			reqSeconds = float64(v)
		}

		if reqSeconds > 0 {
			// Protect the system: Cap the dynamic timeout at 2 hours (7200 seconds)
			if reqSeconds > 7200 {
				reqSeconds = 7200
			}
			activeTimeout = time.Duration(reqSeconds * float64(time.Second))
		}
	}
	return activeTimeout
}

func (d *safetyDecorator) handleTimeout(parentCtx context.Context, errCtx error, toolName string, activeTimeout time.Duration, outCh chan tools.ToolOutput) tools.ToolResult {
	msg := fmt.Sprintf("Error: Tool execution failed: %v", errCtx)
	errorWrapMsg := "tool execution failed"
	switch errCtx {
	case context.Canceled:
		msg = "Execution was interrupted or cancelled by the user."
	case context.DeadlineExceeded:
		msg = fmt.Sprintf("Error: Tool execution timed out after %v", activeTimeout)
		errorWrapMsg = "tool execution timed out"
	}

	go d.zombie.Monitor(context.WithoutCancel(parentCtx), toolName, time.Now(), outCh, d.zombieTimeout)

	return tools.ToolResult{
		Text:  msg,
		Error: fmt.Errorf("%w: %s: %w", llm.ErrTransient, errorWrapMsg, errCtx),
	}
}

func (d *safetyDecorator) monitorLiveness(
	ctx context.Context,
	toolName string,
	opts tools.ToolOptions,
	hbCh <-chan struct{},
	heartbeat chan<- struct{},
	cancel context.CancelFunc,
) {
	timer := newLivenessTimer(opts.LivenessThreshold)
	defer timer.stop()

	for {
		select {
		case v, ok := <-hbCh:
			if !ok {
				return
			}
			// Forward to upper layer if any
			if heartbeat != nil {
				select {
				case heartbeat <- v:
				default:
				}
			}

			timer.reset()
		case <-timer.channel():
			d.logger.Error("tool_liveness_timeout", "tool_name", toolName, "threshold", opts.LivenessThreshold)
			cancel()
			return
		case <-ctx.Done():
			// FIX: Start a background drainer to prevent the tool from blocking on hbCh
			go func() {
				defer func() {
					_ = recover() // Ignore panic, preventing crash on drainer
				}()
				for range hbCh {
					// Draining until hbCh is closed by the tool's defer block
				}
			}()
			return
		}
	}
}

// livenessTimer encapsulates the timer logic for tool liveness checks.
type livenessTimer struct {
	timer     *time.Timer
	threshold time.Duration
}

func newLivenessTimer(threshold time.Duration) *livenessTimer {
	var t *time.Timer
	if threshold > 0 {
		t = time.NewTimer(threshold)
	}
	return &livenessTimer{
		timer:     t,
		threshold: threshold,
	}
}

func (t *livenessTimer) reset() {
	if t.timer == nil {
		return
	}
	if !t.timer.Stop() {
		select {
		case <-t.timer.C:
		default:
		}
	}
	t.timer.Reset(t.threshold)
}

func (t *livenessTimer) stop() {
	if t.timer != nil {
		t.timer.Stop()
	}
}

func (t *livenessTimer) channel() <-chan time.Time {
	if t.timer == nil {
		return nil
	}
	return t.timer.C
}

func (d *safetyDecorator) handlePanic(ctx context.Context, r interface{}, toolName string) tools.ToolResult {
	stack := debug.Stack()
	msg := fmt.Sprintf("CRITICAL: Panic in tool executor while running %q: %v\n%s", toolName, r, string(stack))
	evt := events.SystemMessageEvent{
		Message: msg,
		Level:   "error",
	}
	_ = events.SafePublish(ctx, d.events, evt)

	return tools.ToolResult{
		Text:  fmt.Sprintf("Tool %q encountered an internal fatal error (panic) and was terminated.", toolName),
		Error: fmt.Errorf("%w: Panic detected: %v", llm.ErrTerminal, r),
	}
}

// tracingDecorator handles OTel spans and telemetry tracing.
type tracingDecorator struct {
	next     ToolExecutor
	registry tools.Registry
	logger   ports.Logger
}

func newTracingDecorator(next ToolExecutor, registry tools.Registry, logger ports.Logger) ToolExecutor {
	return &tracingDecorator{next: next, registry: registry, logger: logger}
}

func (d *tracingDecorator) Execute(parentCtx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall, hb chan<- struct{}) (tools.ToolResult, error) {
	ctx, span := otel.Tracer("agent").Start(parentCtx, "tool.execute."+call.Name)
	ctx = domain_security.WithCurrentTool(ctx, call.Name)
	span.SetAttributes(attribute.String("tool.name", call.Name))
	defer span.End()

	startTime := time.Now()
	trace := telemetry.TraceFromContext(ctx)

	result, err := d.next.Execute(ctx, tool, call, hb)

	duration := time.Since(startTime)
	status, _ := classifyToolError(err, result.Error)
	errStr := formatToolExecutionError(err, result.Error)

	isSerial := d.registry.IsSerial(call.Name)
	isLongRunning := d.registry.IsLongRunning(call.Name)

	logAttrs := []any{
		"tool_name", call.Name,
		"is_serial", isSerial,
		"is_long_running", isLongRunning,
		"duration_ms", duration.Milliseconds(),
		"status", status,
	}
	if errStr != "" {
		logAttrs = append(logAttrs, "error_reason", errStr)
	}

	if status == "error" {
		d.logger.Debug("Tool execution failed", logAttrs...)
	} else {
		d.logger.Debug("Tool execution completed", logAttrs...)
	}

	if trace != nil {
		trace.RecordToolExecution(telemetry.ToolExecutionTrace{
			ToolName:  call.Name,
			StartTime: startTime,
			Duration:  duration,
			Status:    status,
			Error:     errStr,
		})
	}

	return result, err
}

func classifyToolError(err error, resultErr error) (string, string) {
	if checkBoth(err, resultErr, isUserDeclined) {
		return "user_declined", "The user explicitly denied this action. Do not attempt this exact action again. Ask the user for clarification or propose an alternative approach."
	}
	if checkBoth(err, resultErr, isSecurityError) {
		return "security_blocked", fmt.Sprintf("Action blocked by the system sandbox security policy: %v", formatToolExecutionError(err, resultErr))
	}
	// Logic remains identical to original: cancellation and other errors both return "error"
	if checkBoth(err, resultErr, isCancellationError) || err != nil || resultErr != nil {
		return "error", ""
	}
	return "success", ""
}

func checkBoth(err1, err2 error, predicate func(error) bool) bool {
	return predicate(err1) || predicate(err2)
}

func isSecurityError(err error) bool {
	if errors.Is(err, tools.ErrSecurityPolicy) || errors.Is(err, domain_security.ErrSandboxViolation) {
		return true
	}
	return false
}

func isUserDeclined(err error) bool {
	return errors.Is(err, tools.ErrUserDeclined)
}

func isCancellationError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func formatToolExecutionError(err error, resultErr error) string {
	if err != nil && resultErr != nil {
		return errors.Join(err, resultErr).Error()
	} else if err != nil {
		return err.Error()
	} else if resultErr != nil {
		return resultErr.Error()
	}
	return ""
}
