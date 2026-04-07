// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
	"github.com/stretchr/testify/require"
)

func TestIntegration_DecoratorKillsProcess(t *testing.T) {
	t.Parallel()

	// 1. Setup real dependencies
	reg := registry.New()
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true) // Bypass prompts
	validator := security.NewCommandValidator(sm, nil)
	fs := persistence.NewOSFileSystem()
	logger := &ports.NoOpLogger{}
	bus := events.NewSimpleEventBus(context.Background())
	mockLog := &mockLogger{}
	zombieTool, _ := tools.NewZombieTool(mockLog)

	// 2. Register the real workspace tools (injects execute_command)
	err := workspace.Register(reg, sm, nil, validator, fs)
	require.NoError(t, err, "failed to register workspace tools")

	// 3. Wire the REAL components together (Base Runtime + Safety Decorator)
	base := newBaseRuntime(reg)

	// Set base timeouts high, but we will override dynamically via the payload
	decorator := newSafetyDecorator(
		base,
		reg,
		logger,
		bus,
		zombieTool,
		30*time.Second,  // default tool timeout
		300*time.Second, // default long running timeout
		5*time.Second,   // zombie timeout
	)

	// 4. Setup args (Decorator parses timeout, Tool runs command)
	// We want to run a 5 second sleep, but give it a 1 second timeout.
	call := &llm.FunctionCall{
		Name: "execute_command",
		Args: map[string]interface{}{
			"command": "sleep 5",
			"reason":  "testing decorator timeout integration",
			"timeout": 1, // dynamically overrides the 300s default in the decorator
		},
	}

	// Fetch the declaration to pass to Execute
	decl := reg.GetCoreDeclarations()[0] // Not strictly used by execute_command but required by signature
	for _, d := range reg.GetCoreDeclarations() {
		if d.Name == "execute_command" {
			decl = d
			break
		}
	}

	start := time.Now()

	// 5. Execute
	hb := make(chan struct{}, 1)
	res, err := decorator.Execute(context.Background(), decl, call, hb)
	elapsed := time.Since(start)

	// 6. Verify actual OS-level termination via elapsed time
	require.NoError(t, err, "decorator itself shouldn't fail fatally")
	require.Error(t, res.Error, "expected res.Error from decorator execution")

	// Flaky-safe timing check (allow OS/CI overhead, but ensure it didn't sleep for 5s)
	require.Less(t, elapsed, 3*time.Second, "OS process was not terminated by the decorator's context")

	// 7. Verify domain boundary (Error translation)
	require.True(t, errors.Is(res.Error, llm.ErrTransient), "Expected timeout to be translated to a transient error")
	require.Contains(t, res.Text, "timed out after", "Result text should indicate timeout")
}

func TestIntegration_DecoratorKillsPipeline(t *testing.T) {
	t.Parallel()

	// 1. Setup real dependencies
	reg := registry.New()
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true) // Bypass prompts
	validator := security.NewCommandValidator(sm, nil)
	fs := persistence.NewOSFileSystem()
	logger := &ports.NoOpLogger{}
	bus := events.NewSimpleEventBus(context.Background())
	mockLog := &mockLogger{}
	zombieTool, _ := tools.NewZombieTool(mockLog)

	// 2. Register the real workspace tools (injects pipe_commands)
	err := workspace.Register(reg, sm, nil, validator, fs)
	require.NoError(t, err, "failed to register workspace tools")

	// 3. Wire the REAL components together (Base Runtime + Safety Decorator)
	base := newBaseRuntime(reg)

	decorator := newSafetyDecorator(
		base,
		reg,
		logger,
		bus,
		zombieTool,
		30*time.Second,  // default tool timeout
		300*time.Second, // default long running timeout
		5*time.Second,   // zombie timeout
	)

	// 4. Setup args
	call := &llm.FunctionCall{
		Name: "pipe_commands",
		Args: map[string]interface{}{
			"commands": []string{"sleep 5", "cat"},
			"reason":   "testing decorator timeout integration for pipes",
			"timeout":  1, // dynamically overrides the 300s default
		},
	}

	decl := reg.GetCoreDeclarations()[0]
	for _, d := range reg.GetCoreDeclarations() {
		if d.Name == "pipe_commands" {
			decl = d
			break
		}
	}

	start := time.Now()

	// 5. Execute
	hb := make(chan struct{}, 1)
	res, err := decorator.Execute(context.Background(), decl, call, hb)
	elapsed := time.Since(start)

	// 6. Verify actual OS-level termination via elapsed time
	require.NoError(t, err, "decorator itself shouldn't fail fatally")
	require.Error(t, res.Error, "expected res.Error from decorator execution")

	require.Less(t, elapsed, 3*time.Second, "OS pipeline was not terminated by the decorator's context")

	// 7. Verify domain boundary (Error translation)
	require.True(t, errors.Is(res.Error, llm.ErrTransient), "Expected timeout to be translated to a transient error")
	require.Contains(t, res.Text, "timed out after", "Result text should indicate timeout")
}
