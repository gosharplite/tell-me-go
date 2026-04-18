// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file pins the contract for validateRequiredArgs and the
// registry.Execute integration that calls it. The central guard ensures
// every tool benefits from required-parameter enforcement without the
// per-handler boilerplate that otherwise has to be (and was historically)
// repeated in 70+ handlers.
//
// The guard's contract has three load-bearing properties, each with a
// dedicated test below:
//
//  1. Missing required parameter → ToolResult{Text: "Error: ..."}, nil
//     (model-friendly, matches the prevailing convention used by
//     generateMermaidDiagram and other handlers in the codebase).
//  2. Explicitly-empty value (key present, value "") is allowed: this
//     distinguishes "intentionally empty" from "forgotten" — the same
//     distinction documented in the per-handler guard comment in
//     internal/tools/workspace/writer.go.
//  3. Handler is NOT invoked when the guard fires; no I/O, no side
//     effects, no logging from the tool itself.
//
// FAILURE MEANING: If any test in this file regresses, the central
// required-args guard has been weakened or removed. Restore it; do not
// "fix" by deleting these tests. The guard exists to make a class of
// silent-failure bug structurally impossible.

package registry_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guardedTool builds a registry containing a single tool with the given
// Required schema. The handler increments callCount each invocation, so
// tests can assert whether the handler was reached.
func guardedTool(t *testing.T, required []string) (tools.Registry, *atomic.Int64) {
	t.Helper()
	var callCount atomic.Int64
	reg := registry.New()
	err := reg.Register(&tools.ToolDeclaration{
		Name: "test_tool",
		Parameters: &tools.Schema{
			Type:     "OBJECT",
			Required: required,
		},
	}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		callCount.Add(1)
		return tools.ToolResult{Text: "handler invoked"}, nil
	})
	require.NoError(t, err)
	return reg, &callCount
}

// TestRequiredArgsGuard_MissingSingleParameter pins property (1) for the
// single-missing-key path and property (3) for handler-not-invoked.
func TestRequiredArgsGuard_MissingSingleParameter(t *testing.T) {
	t.Parallel()
	reg, callCount := guardedTool(t, []string{"filepath", "content", "reason"})

	// "content" is absent — should be rejected by the guard.
	res, err := reg.Execute(context.Background(), "test_tool", map[string]interface{}{
		"filepath": "/tmp/foo",
		"reason":   "testing",
	}, nil)

	assert.NoError(t, err,
		"missing-required must NOT return an error from Execute; the "+
			"convention is to return a model-friendly ToolResult{Text: "+
			"\"Error: ...\"} instead, so the LLM sees the failure as a "+
			"tool result it can react to. See registry.Execute doc.")
	assert.Contains(t, res.Text, "Error:",
		"result text must start with the conventional 'Error:' prefix\n"+
			"got: %q", res.Text)
	assert.Contains(t, res.Text, "content",
		"result text must name the missing parameter so the model can "+
			"correct its next call\ngot: %q", res.Text)
	assert.Contains(t, res.Text, "test_tool",
		"result text must name the tool so the model has full context\n"+
			"got: %q", res.Text)
	assert.Equal(t, int64(0), callCount.Load(),
		"handler must NOT be invoked when the guard fires — this is the "+
			"property that makes the guard load-bearing. If this fails, "+
			"the guard runs AFTER the handler instead of before, defeating "+
			"its purpose.")
}

// TestRequiredArgsGuard_MissingMultipleParameters pins property (1) for
// the multi-missing-key path. The error message must list ALL missing
// keys, not just the first, so the model can fix everything in one
// retry rather than discovering missing keys one-by-one across multiple
// turns (which would burn tokens and time).
func TestRequiredArgsGuard_MissingMultipleParameters(t *testing.T) {
	t.Parallel()
	reg, callCount := guardedTool(t, []string{"filepath", "content", "reason"})

	// All three required keys are missing.
	res, err := reg.Execute(context.Background(), "test_tool", map[string]interface{}{}, nil)

	assert.NoError(t, err)
	assert.Contains(t, res.Text, "Error:")
	for _, key := range []string{"filepath", "content", "reason"} {
		assert.Contains(t, res.Text, key,
			"all missing keys must be reported in a single error so the "+
				"model can correct everything in one retry. Missing %q "+
				"from result text: %q", key, res.Text)
	}
	assert.Equal(t, int64(0), callCount.Load(),
		"handler must not be invoked")
}

// TestRequiredArgsGuard_ExplicitEmptyValueIsAllowed pins property (2) —
// the load-bearing distinction between "key present, value zero" (intent)
// and "key absent" (bug). Without this, callers could not legitimately
// pass content="" to write_file to create a 0-byte file.
func TestRequiredArgsGuard_ExplicitEmptyValueIsAllowed(t *testing.T) {
	t.Parallel()
	reg, callCount := guardedTool(t, []string{"content"})

	res, err := reg.Execute(context.Background(), "test_tool", map[string]interface{}{
		"content": "", // present-but-empty must pass the guard.
	}, nil)

	assert.NoError(t, err)
	assert.Equal(t, "handler invoked", res.Text,
		"present-but-empty value must reach the handler (not be rejected "+
			"by the guard). Without this, `write_file` could not write "+
			"empty files. Got: %q", res.Text)
	assert.Equal(t, int64(1), callCount.Load(),
		"handler must be invoked exactly once")
}

// TestRequiredArgsGuard_NoRequiredListPasses confirms tools with no
// Required declaration (or no Parameters at all) are not penalized. This
// covers tools like get_git_status, check_system_health, etc.
func TestRequiredArgsGuard_NoRequiredListPasses(t *testing.T) {
	t.Parallel()

	t.Run("nil Parameters", func(t *testing.T) {
		t.Parallel()
		var callCount atomic.Int64
		reg := registry.New()
		require.NoError(t, reg.Register(&tools.ToolDeclaration{
			Name:       "no_params_tool",
			Parameters: nil,
		}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			callCount.Add(1)
			return tools.ToolResult{Text: "ok"}, nil
		}))

		res, err := reg.Execute(context.Background(), "no_params_tool", nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, "ok", res.Text)
		assert.Equal(t, int64(1), callCount.Load())
	})

	t.Run("empty Required slice", func(t *testing.T) {
		t.Parallel()
		reg, callCount := guardedTool(t, []string{}) // Required is empty
		res, err := reg.Execute(context.Background(), "test_tool", nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, "handler invoked", res.Text)
		assert.Equal(t, int64(1), callCount.Load())
	})
}

// TestRequiredArgsGuard_HandlerErrorStillWrapped is a negative control:
// when the guard passes and the handler then returns a real error, the
// pre-existing error-wrapping behavior of Execute (`tool execution
// failed: NAME: ERR`) must be preserved. The guard must not eat or
// alter handler errors.
func TestRequiredArgsGuard_HandlerErrorStillWrapped(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	require.NoError(t, reg.Register(&tools.ToolDeclaration{
		Name: "broken_tool",
		Parameters: &tools.Schema{
			Type:     "OBJECT",
			Required: []string{"x"},
		},
	}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, assert.AnError
	}))

	_, err := reg.Execute(context.Background(), "broken_tool", map[string]interface{}{"x": "y"}, nil)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "tool execution failed"),
		"Execute must still wrap handler errors with 'tool execution "+
			"failed: NAME: ERR'. The guard must be transparent on the "+
			"happy path. Got: %v", err)
	assert.True(t, strings.Contains(err.Error(), "broken_tool"),
		"wrapped error must include the tool name. Got: %v", err)
}
