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

func (d *authDecorator) Execute(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) (tools.ToolResult, error) {
	if err := d.auth.Authorize(ctx, tool, call); err != nil {
		return tools.ToolResult{Text: err.Error(), Error: err}, nil
	}

	return d.next.Execute(ctx, tool, call)
}

// circuitBreakerDecorator handles circuit breaking logic.
type circuitBreakerDecorator struct {
	next ToolExecutor
	cb   CircuitBreakerManager
}

func newCircuitBreakerDecorator(next ToolExecutor, cb CircuitBreakerManager) ToolExecutor {
	return &circuitBreakerDecorator{next: next, cb: cb}
}

func (d *circuitBreakerDecorator) Execute(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) (tools.ToolResult, error) {
	if err := d.cb.Check(call.Name); err != nil {
		return tools.ToolResult{Text: err.Error(), Error: err}, nil
	}

	result, err := d.next.Execute(ctx, tool, call)
	d.cb.Record(call.Name, err == nil && result.Error == nil)
	return result, err
}

// safetyDecorator handles timeouts, panics, and zombie detection.
type safetyDecorator struct {
	next               ToolExecutor
	registry           tools.Registry
	logger             ports.Logger
	events             events.EventBus
	zombie             *tools.ZombieTool
	toolTimeout        func() time.Duration
	longRunningTimeout func() time.Duration
	zombieTimeout      func() time.Duration
}

func newSafetyDecorator(next ToolExecutor, registry tools.Registry, logger ports.Logger, bus events.EventBus, zombie *tools.ZombieTool, toolTimeout, longRunningTimeout, zombieTimeout func() time.Duration) ToolExecutor {
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

func (d *safetyDecorator) Execute(parentCtx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) (result tools.ToolResult, err error) {
	activeTimeout := d.toolTimeout()
	if d.registry.IsLongRunning(call.Name) {
		activeTimeout = d.longRunningTimeout()
	}

	ctx, cancel := context.WithTimeout(parentCtx, activeTimeout)
	defer cancel()

	outCh := make(chan tools.ToolOutput, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				outCh <- tools.ToolOutput{
					Result: d.handlePanic(ctx, r, call.Name),
				}
			}
		}()
		res, execErr := d.next.Execute(ctx, tool, call)
		outCh <- tools.ToolOutput{Result: res, Err: execErr}
	}()

	select {
	case <-ctx.Done():
		errCtx := ctx.Err()
		msg := fmt.Sprintf("Error: Tool execution failed: %v", errCtx)
		errorWrapMsg := "tool execution failed"
		if errCtx == context.DeadlineExceeded {
			msg = fmt.Sprintf("Error: Tool execution timed out after %v", activeTimeout)
			errorWrapMsg = "tool execution timed out"
		}

		go d.zombie.Monitor(parentCtx, call.Name, time.Now(), outCh, d.zombieTimeout())

		return tools.ToolResult{
			Text:  msg,
			Error: fmt.Errorf("%w: %s: %w", llm.ErrTransient, errorWrapMsg, errCtx),
		}, nil

	case out := <-outCh:
		return out.Result, out.Err
	}
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

func (d *tracingDecorator) Execute(parentCtx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) (tools.ToolResult, error) {
	ctx, span := otel.Tracer("agent").Start(parentCtx, "tool.execute."+call.Name)
	ctx = domain_security.WithCurrentTool(ctx, call.Name)
	span.SetAttributes(attribute.String("tool.name", call.Name))
	defer span.End()

	startTime := time.Now()
	trace := telemetry.TraceFromContext(ctx)

	result, err := d.next.Execute(ctx, tool, call)

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

	if status == "error" || status == "circuit_open" {
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
	if errors.Is(err, tools.ErrUserDeclined) || (resultErr != nil && errors.Is(resultErr, tools.ErrUserDeclined)) {
		return "user_declined", "The user explicitly denied this action. Do not attempt this exact action again. Ask the user for clarification or propose an alternative approach."
	}
	if errors.Is(err, tools.ErrSecurityPolicy) || (resultErr != nil && errors.Is(resultErr, tools.ErrSecurityPolicy)) {
		return "security_blocked", "Action blocked by the system sandbox security policy. You are not authorized to perform this operation."
	}
	if errors.Is(err, tools.ErrToolCircuitOpen) || (resultErr != nil && errors.Is(resultErr, tools.ErrToolCircuitOpen)) {
		return "circuit_open", ""
	}
	if err != nil || resultErr != nil {
		return "error", ""
	}
	return "success", ""
}

func formatToolExecutionError(err error, resultErr error) string {
	if err != nil {
		return err.Error()
	} else if resultErr != nil {
		return resultErr.Error()
	}
	return ""
}
