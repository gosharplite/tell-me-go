// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

// Chatter defines the interface for the AI agent orchestration.
type Chatter interface {
	Chat(ctx context.Context, s *Session, prompt string) error
	SetLogFile(path string)
	SetLimits(toolTurns, historyTokens, historyTurns int)
	SetTieredThreshold(threshold int)
	SetPrunedTurns(n int)
	SetConcurrency(maxConcurrent int, timeoutSeconds int)
	SetPersistentConfigPath(path string)
	SetMainConfigPath(path string)
	Subscribe(sub func(events.Event))
}

// RuntimeConfig consolidates all agent configuration parameters.
type RuntimeConfig struct {
	Limits               events.Limits
	Execution            events.ExecutionConfig
	LogFile              string
	PersistentConfigPath string
	MainConfigPath       string
}

// Agent represents the chat orchestration logic (Stateless Service).
type Agent struct {
	mu            sync.RWMutex
	gateway       *gateway.ResilientClient
	engine        *TurnEngine
	ctxManager    *ContextManager
	registry      *registry.Registry
	sm            *security.SecurityManager
	configWatcher *ConfigWatcher
	strategy      *ContextStrategy
	executor      *executor.ToolExecutor
	events        events.EventBus

	config RuntimeConfig
}

// AgentOption defines a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithLimits sets the initial operational limits.
func WithLimits(toolTurns, historyTokens, historyTurns int) AgentOption {
	return func(a *Agent) {
		a.config.Limits = events.Limits{
			MaxHistoryTokens: historyTokens,
			MaxToolTurns:     toolTurns,
			MaxHistoryTurns:  historyTurns,
		}
		if a.configWatcher != nil {
			a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
		}
	}
}

// WithConcurrency sets the parallel execution limits for the agent.
func WithConcurrency(maxConcurrent int, timeoutSeconds int) AgentOption {
	return func(a *Agent) {
		a.config.Execution = events.ExecutionConfig{
			MaxConcurrent: maxConcurrent,
			Timeout:       time.Duration(timeoutSeconds) * time.Second,
		}
	}
}

// WithLogFile sets the path for usage logging.
func WithLogFile(path string) AgentOption {
	return func(a *Agent) {
		a.config.LogFile = path
	}
}

// WithPersistentConfigPath sets the path to the persistent session configuration.
func WithPersistentConfigPath(path string) AgentOption {
	return func(a *Agent) {
		a.config.PersistentConfigPath = path
	}
}

// WithMainConfigPath sets the path to the main YAML configuration file.
func WithMainConfigPath(path string) AgentOption {
	return func(a *Agent) {
		a.config.MainConfigPath = path
	}
}

// New creates a new Agent using functional options.
func New(client llm.LLMClient, hManager *history.Manager, reg *registry.Registry, sm *security.SecurityManager, disableStreaming bool, options ...AgentOption) *Agent {
	bus := &events.SimpleEventBus{}
	gw := gateway.NewResilientClient(client, disableStreaming)

	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), bus)
	exec := executor.NewToolExecutor(reg, sm, bus)

	factory := &PipelineFactory{
		Registry:   reg,
		History:    hManager,
		Summarizer: NewSummarizer(gw, bus),
		Estimator:  strategy,
		Events:     bus,
	}

	ctxManager := NewContextManager(strategy, hManager, gw, bus, factory)

	a := &Agent{
		gateway:       gw,
		ctxManager:    ctxManager,
		registry:      reg,
		sm:            sm,
		configWatcher: NewConfigWatcher(config.DefaultMaxHistoryTokens, config.DefaultMaxToolTurns, config.DefaultMaxHistoryTurns),
		strategy:      strategy,
		executor:      exec,
		events:        bus,
		config: RuntimeConfig{
			Limits: events.Limits{
				MaxHistoryTokens: config.DefaultMaxHistoryTokens,
				MaxToolTurns:     config.DefaultMaxToolTurns,
				MaxHistoryTurns:  config.DefaultMaxHistoryTurns,
			},
			Execution: events.ExecutionConfig{
				MaxConcurrent: config.DefaultMaxConcurrentTools,
				Timeout:       time.Duration(config.DefaultToolTimeoutSeconds) * time.Second,
			},
		},
	}

	// Apply options
	for _, opt := range options {
		opt(a)
	}

	// Initialize engine
	a.engine = NewTurnEngine(gw, exec, ctxManager, reg, bus)

	a.registerInternalTools()
	a.applyConfig() // Broadcast initial config
	return a
}

func (a *Agent) applyConfig() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.configWatcher.SetPaths(a.config.MainConfigPath, a.config.PersistentConfigPath)
	a.configWatcher.Refresh()

	tokens, toolTurns, histTurns := a.configWatcher.GetLimits()
	threshold := a.configWatcher.GetTieredThreshold()

	// Update config from watcher if it changed
	a.config.Limits.MaxHistoryTokens = tokens
	a.config.Limits.MaxToolTurns = toolTurns
	a.config.Limits.MaxHistoryTurns = histTurns
	a.config.Limits.TieredThreshold = threshold

	a.events.Publish(events.ConfigUpdated{
		Limits:    a.config.Limits,
		LogFile:   a.config.LogFile,
		Execution: a.config.Execution,
	})
}

func (a *Agent) Subscribe(sub func(events.Event)) {
	a.events.Subscribe(sub)
}

func (a *Agent) emit(e events.Event) {
	a.events.Publish(e)
}

func (a *Agent) registerInternalTools() {
	it := NewInternalTools(a.ctxManager)
	a.registry.Register(&tools.ToolDeclaration{
		Name:        "summarize_history",
		Description: "Summarizes a specified number of older conversation turns to free up context space.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"turns": {
					Type:        "NUMBER",
					Description: "The number of turns (user+model pairs) to summarize from the beginning of history.",
				},
				"focus": {
					Type:        "STRING",
					Description: "Optional: Specific aspects to focus on in the summary (e.g., 'architecture decisions').",
				},
			},
			Required: []string{"turns"},
		},
	}, it.SummarizeHistory)
}

// SetLimits sets the operational limits for the agent.
func (a *Agent) SetLimits(toolTurns, historyTokens, historyTurns int) {
	a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
	a.applyConfig()
}

func (a *Agent) SetTieredThreshold(threshold int) {
	a.configWatcher.SetTieredThreshold(threshold)
	a.applyConfig()
}

// SetConcurrency sets the parallel execution limits for the agent.
func (a *Agent) SetConcurrency(maxConcurrent int, timeoutSeconds int) {
	a.mu.Lock()
	a.config.Execution.MaxConcurrent = maxConcurrent
	a.config.Execution.Timeout = time.Duration(timeoutSeconds) * time.Second
	a.mu.Unlock()
	a.applyConfig()
}

// SetLogFile sets the path for usage logging.
func (a *Agent) SetLogFile(path string) {
	a.mu.Lock()
	a.config.LogFile = path
	a.mu.Unlock()
	a.applyConfig()
}

// SetPrunedTurns (Legacy support - usually in Session)
func (a *Agent) SetPrunedTurns(n int) {
	a.strategy.SetPrunedTurns(n)
}

// SetPersistentConfigPath sets the path to the persistent session configuration.
func (a *Agent) SetPersistentConfigPath(path string) {
	a.mu.Lock()
	a.config.PersistentConfigPath = path
	a.mu.Unlock()
	a.applyConfig()
}

// SetMainConfigPath sets the path to the main YAML configuration file.
func (a *Agent) SetMainConfigPath(path string) {
	a.mu.Lock()
	a.config.MainConfigPath = path
	a.mu.Unlock()
	a.applyConfig()
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(ctx context.Context, s *Session, prompt string) error {
	if err := s.History.AddContent(ctx, &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	a.applyConfig()
	a.emit(events.StatusUpdate{Message: "Starting chat...", Level: "info"})
	return a.engine.Run(ctx, s.StartTime)
}
