// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Warning represents a safety or limit message for the model.
type Warning struct {
	Message string
}

// ContextStrategy handles token estimation and warning generation.
type ContextStrategy struct {
	mu               sync.RWMutex
	counter          llm.TokenCounter
	maxHistoryTokens int
	maxToolTurns     int
	maxHistoryTurns  int
	tieredThreshold  int
	prunedTurns      int
}

// ToolRegistry defines the interface for accessing tool declarations.
type ToolRegistry interface {
	GetDeclarations() []*tools.ToolDeclaration
}

// NewContextStrategy creates a new context strategy.
func NewContextStrategy(counter llm.TokenCounter, bus events.EventBus) *ContextStrategy {
	defaultThreshold := config.DefaultTieredThreshold
	if dp := config.DefaultPricing(); dp.Models != nil {
		if m, ok := dp.Models["default"]; ok && m.TieredThreshold > 0 {
			defaultThreshold = int(m.TieredThreshold)
		}
	}

	cs := &ContextStrategy{
		counter:          counter,
		maxHistoryTokens: config.DefaultMaxHistoryTokens,
		maxToolTurns:     config.DefaultMaxToolTurns,
		maxHistoryTurns:  config.DefaultMaxHistoryTurns,
		tieredThreshold:  defaultThreshold,
	}

	if bus != nil {
		bus.Subscribe(func(e events.Event) {
			if cfg, ok := e.(events.ConfigUpdated); ok {
				cs.SetLimits(cfg.Limits.MaxHistoryTokens, cfg.Limits.MaxToolTurns, cfg.Limits.MaxHistoryTurns)
				cs.SetTieredThreshold(cfg.Limits.TieredThreshold)
			}
		})
	}

	return cs
}

// SetLimits updates the operational limits.
func (cs *ContextStrategy) SetLimits(historyTokens, toolTurns, historyTurns int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if historyTokens > 0 {
		cs.maxHistoryTokens = historyTokens
	}
	if toolTurns > 0 {
		cs.maxToolTurns = toolTurns
	}
	if historyTurns > 0 {
		cs.maxHistoryTurns = historyTurns
	}
}

func (cs *ContextStrategy) SetTieredThreshold(threshold int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if threshold >= 0 {
		cs.tieredThreshold = threshold
	}
}

// SetPrunedTurns sets the initial pruned turns count.
func (cs *ContextStrategy) SetPrunedTurns(n int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.prunedTurns = n
}

// GetLimits returns the current limits.
func (cs *ContextStrategy) GetLimits() (int, int, int) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.maxHistoryTokens, cs.maxToolTurns, cs.maxHistoryTurns
}

func (cs *ContextStrategy) GetTieredThreshold() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.tieredThreshold
}

// EstimateTokens provides a heuristic-based token count with incremental caching.
func (cs *ContextStrategy) EstimateTokens(contents []*llm.Content) int {
	return cs.counter.Count(contents)
}

func (cs *ContextStrategy) getTurnWarning(turn int) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.getTurnWarningLocked(turn)
}

func (cs *ContextStrategy) getTokenWarning(tokens int) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.getTokenWarningLocked(tokens)
}

func (cs *ContextStrategy) getHistoryTurnWarning(currentTurns int) string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.getHistoryTurnWarningLocked(currentTurns)
}

// GetWarnings generates safety and financial warnings based on current state.
func (cs *ContextStrategy) GetWarnings(turn, tokens, currentTurns int) []Warning {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var warnings []Warning

	if w := cs.getTurnWarningLocked(turn); w != "" {
		warnings = append(warnings, Warning{Message: w})
	}
	if w := cs.getTokenWarningLocked(tokens); w != "" {
		warnings = append(warnings, Warning{Message: w})
	}
	if w := cs.getHistoryTurnWarningLocked(currentTurns); w != "" {
		warnings = append(warnings, Warning{Message: w})
	}
	if w := cs.getPriceWarningLocked(tokens); w != "" {
		warnings = append(warnings, Warning{Message: w})
	}

	return warnings
}

func (cs *ContextStrategy) getPriceWarningLocked(tokens int) string {
	cliff := cs.tieredThreshold
	if cliff <= 0 {
		return ""
	}
	warning := int(float64(cliff) * config.WarningRatio)

	if tokens >= cliff {
		return "[URGENT ECONOMIC NOTICE: The high-tier billing threshold has been reached. Current operational costs are now 2x higher. You MUST be extremely concise, minimize internal reasoning (Thinking Tokens), and combine multiple operations into single turns where possible to conserve the user's budget.]"
	} else if tokens >= warning {
		return "[ECONOMIC NOTICE: You are approaching the high-tier billing threshold. To protect the user's budget, please be highly selective with tool calls and avoid redundant operations. Focus on high-impact actions and be concise in your reasoning.]"
	}
	return ""
}

func (cs *ContextStrategy) getTurnWarningLocked(turn int) string {
	remaining := cs.maxToolTurns - turn
	switch remaining {
	case 3:
		return "[SYSTEM NOTICE: You are approaching the operational turn limit (3 turns remaining). Please begin finalizing your current task and use this turn to perform any final state checks or file reads needed for your summary.]"
	case 2:
		return "[URGENT SYSTEM NOTICE: Only 2 turns remain. You MUST now use 'manage_scratchpad' and 'manage_tasks' to document the distilled state of the project (architecture, progress, and next steps). This ensures context efficiency and continuity for the user in future sessions, as conversation history may be pruned.]"
	case 1:
		return "[FINAL SYSTEM WARNING: This is your absolute final turn. You are forbidden from using any more tools. Provide a concise final conclusion or progress summary to the user now. Execution will terminate immediately after this response.]"
	default:
		return ""
	}
}

func (cs *ContextStrategy) getTokenWarningLocked(tokens int) string {
	ratio := float64(tokens) / float64(cs.maxHistoryTokens)
	if ratio > 0.95 {
		return "[CRITICAL SYSTEM NOTICE: Conversation history is at 95% capacity. Immediate risk of session rollback. You must use 'manage_scratchpad' and 'manage_tasks' to save a summary of your work and plans NOW. Keep your response extremely brief.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: The conversation history is at 90% capacity. To avoid a session crash, please minimize large file reads. Use 'manage_scratchpad' and 'manage_tasks' to save your current progress and architectural notes now, in case a rollback occurs.]"
	}
	return ""
}

func (cs *ContextStrategy) getHistoryTurnWarningLocked(currentTurns int) string {
	if cs.maxHistoryTurns <= 0 {
		return ""
	}

	if cs.prunedTurns > 5 {
		msg := fmt.Sprintf("[URGENT SYSTEM NOTICE: A major history cleanup has occurred. To maintain performance and cache efficiency, the oldest %d turns of this conversation have been removed. You have lost significant recent context. You MUST refer to the 'manage_scratchpad' and read 'manage_tasks' to continue unfinished tasks and re-synchronize your internal state.]", cs.prunedTurns)
		cs.prunedTurns = 0
		return msg
	}

	ratio := float64(currentTurns) / float64(cs.maxHistoryTurns)
	if ratio >= 1.0 {
		return "[SYSTEM NOTICE: The history turn limit has been reached and the oldest messages in this conversation have been deleted. If you are missing previous context or architectural details, please refer to 'manage_scratchpad' and 'manage_tasks' for the latest status and pending tasks.]"
	} else if ratio > 0.95 {
		return "[URGENT SYSTEM NOTICE: Conversation history is at 95% of the turn limit. Pruning is imminent. The oldest messages in this thread will be DELETED after this turn. Move all essential long-term memory to the scratchpad and task list immediately.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: Conversation history is at 90% of the turn limit. To prevent loss of context during upcoming pruning, ensure critical architectural decisions and progress are documented in the scratchpad and 'manage_tasks'.]"
	}
	return ""
}
