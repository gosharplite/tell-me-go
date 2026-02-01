// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Warning represents a safety or limit message for the model.
type Warning struct {
	Message string
}

// ContextStrategy handles token estimation and warning generation.
type ContextStrategy struct {
	registry         ToolRegistry
	maxHistoryTokens int
	maxToolTurns     int
	maxHistoryTurns  int
	prunedTurns      int
}

// ToolRegistry defines the interface for accessing tool declarations.
type ToolRegistry interface {
	GetDeclarations() []*tools.ToolDeclaration
}

// NewContextStrategy creates a new context strategy.
func NewContextStrategy(registry ToolRegistry, bus events.EventBus) *ContextStrategy {
	cs := &ContextStrategy{
		registry:         registry,
		maxHistoryTokens: 120000,
		maxToolTurns:     10,
		maxHistoryTurns:  20,
	}

	if bus != nil {
		bus.Subscribe(func(e events.Event) {
			if cfg, ok := e.(events.ConfigUpdated); ok {
				cs.SetLimits(cfg.Limits.MaxHistoryTokens, cfg.Limits.MaxToolTurns, cfg.Limits.MaxHistoryTurns)
			}
		})
	}

	return cs
}

// SetLimits updates the operational limits.
func (cs *ContextStrategy) SetLimits(historyTokens, toolTurns, historyTurns int) {
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

// SetPrunedTurns sets the initial pruned turns count.
func (cs *ContextStrategy) SetPrunedTurns(n int) {
	cs.prunedTurns = n
}

// GetLimits returns the current limits.
func (cs *ContextStrategy) GetLimits() (int, int, int) {
	return cs.maxHistoryTokens, cs.maxToolTurns, cs.maxHistoryTurns
}

// EstimateTokens provides a heuristic-based token count with incremental caching.
func (cs *ContextStrategy) EstimateTokens(contents []*llm.Content) int {
	totalTokens := 0

	// Overhead for tools
	for _, decl := range cs.registry.GetDeclarations() {
		totalTokens += (len(decl.Name) + len(decl.Description)) / 4
		if decl.Parameters != nil {
			totalTokens += 50 // Heuristic for parameter definitions
		}
	}

	for _, c := range contents {
		if c.TokenCount > 0 {
			totalTokens += c.TokenCount
			continue
		}

		// Calculate delta for this content
		charCount := 0
		for _, p := range c.Parts {
			if p.Text != "" {
				charCount += len(p.Text)
			}
			if p.FunctionCall != nil {
				charCount += len(p.FunctionCall.Name)
				charCount += cs.estimateMapSize(p.FunctionCall.Args)
			}
			if p.FunctionResponse != nil {
				charCount += len(p.FunctionResponse.Name)
				charCount += cs.estimateMapSize(p.FunctionResponse.Response)
			}
			if p.InlineData != nil {
				charCount += 160 // Heuristic for blob (roughly 50 tokens)
			}
		}

		c.TokenCount = int(float64(charCount) / 3.2)
		totalTokens += c.TokenCount
	}

	totalTokens += 300 // Base overhead
	return totalTokens
}

func (cs *ContextStrategy) estimateMapSize(m map[string]interface{}) int {
	if m == nil {
		return 0
	}
	size := 0
	for k, v := range m {
		size += len(k)
		size += cs.estimateValueSize(v)
	}
	return size
}

func (cs *ContextStrategy) estimateValueSize(v interface{}) int {
	if v == nil {
		return 4
	}
	switch val := v.(type) {
	case string:
		return len(val)
	case float64, int, int64:
		return 10
	case bool:
		return 5
	case map[string]interface{}:
		return cs.estimateMapSize(val)
	case []interface{}:
		size := 0
		for _, item := range val {
			size += cs.estimateValueSize(item)
		}
		return size
	default:
		return 20
	}
}

// GetWarnings generates safety warnings based on current state.
func (cs *ContextStrategy) GetWarnings(turn, tokens, currentTurns int) []Warning {
	var warnings []Warning

	if w := cs.getTurnWarning(turn); w != "" {
		warnings = append(warnings, Warning{Message: w})
	}
	if w := cs.getTokenWarning(tokens); w != "" {
		warnings = append(warnings, Warning{Message: w})
	}
	if w := cs.getHistoryTurnWarning(currentTurns); w != "" {
		warnings = append(warnings, Warning{Message: w})
	}

	return warnings
}

func (cs *ContextStrategy) getTurnWarning(turn int) string {
	remaining := cs.maxToolTurns - turn
	switch remaining {
	case 3:
		return "[SYSTEM NOTICE: You are approaching the operational turn limit. You have 3 turns remaining. Please begin finalizing your current task, update the scratchpad and task list with your status, and avoid starting any new multi-step operations.]"
	case 2:
		return "[URGENT SYSTEM NOTICE: Operational limit imminent. Only 2 turns remaining. You must prioritize completing the current objective or documenting progress. You MUST document unfinished sub-tasks in 'manage_tasks' now. New tool sequences will be cut off.]"
	case 1:
		return "[FINAL SYSTEM WARNING: This is your absolute final turn. Provide your final conclusion or progress summary now. Process execution will terminate after this response.]"
	default:
		return ""
	}
}

func (cs *ContextStrategy) getTokenWarning(tokens int) string {
	ratio := float64(tokens) / float64(cs.maxHistoryTokens)
	if ratio > 0.95 {
		return "[CRITICAL SYSTEM NOTICE: Conversation history is at 95% capacity. Immediate risk of session rollback. You must use 'manage_scratchpad' and 'manage_tasks' to save a summary of your work and plans NOW. Keep your response extremely brief.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: The conversation history is at 90% capacity. To avoid a session crash, please minimize large file reads. Use 'manage_scratchpad' and 'manage_tasks' to save your current progress and architectural notes now, in case a rollback occurs.]"
	}
	return ""
}

func (cs *ContextStrategy) getHistoryTurnWarning(currentTurns int) string {
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
