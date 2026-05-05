// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// warning represents a safety or limit message for the model.
type warning struct {
	Message string
}

// Strategy handles token estimation and warning generation.
type Strategy struct {
	mu               sync.RWMutex
	counter          llm.TokenCounter
	maxHistoryTokens int
	maxToolTurns     int
	maxHistoryTurns  int
	contextWindow    int
}

// NewStrategy creates a new context strategy.
func NewStrategy(counter llm.TokenCounter) *Strategy {
	defaultWindow := 1000000 // Default to 1M if unknown

	cs := &Strategy{
		counter:          counter,
		maxHistoryTokens: config.DefaultMaxHistoryTokens,
		maxToolTurns:     config.DefaultMaxToolTurns,
		maxHistoryTurns:  config.DefaultMaxHistoryTurns,
		contextWindow:    defaultWindow,
	}

	return cs
}

// SetContextWindow updates the model's absolute context window limit.
func (cs *Strategy) SetContextWindow(window int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if window > 0 {
		cs.contextWindow = window
	}
}

// getContextWindow returns the model's absolute context window limit.
func (cs *Strategy) getContextWindow() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.contextWindow
}

// SetLimits updates the operational limits.
func (cs *Strategy) SetLimits(historyTokens, toolTurns, historyTurns int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if historyTokens >= 0 {
		cs.maxHistoryTokens = historyTokens
	}
	if toolTurns >= 0 {
		cs.maxToolTurns = toolTurns
	}
	if historyTurns >= 0 {
		cs.maxHistoryTurns = historyTurns
	}
}

// getLimits returns the current limits.
func (cs *Strategy) getLimits() (int, int, int) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.maxHistoryTokens, cs.maxToolTurns, cs.maxHistoryTurns
}

// EstimateTokens provides a heuristic-based token count with incremental caching.
func (cs *Strategy) EstimateTokens(contents []*llm.Content) int {
	return cs.counter.Count(contents)
}

// Count implements llm.TokenCounter.
func (cs *Strategy) Count(contents []*llm.Content) int {
	return cs.EstimateTokens(contents)
}

// getWarnings generates safety and financial warnings based on current state.
func (cs *Strategy) getWarnings(turn, tokens, currentTurns, prunedTurns int) []warning {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	var warnings []warning

	if w := cs.getTurnWarningLocked(turn); w != "" {
		warnings = append(warnings, warning{Message: w})
	}
	if w := cs.getTokenWarningLocked(tokens); w != "" {
		warnings = append(warnings, warning{Message: w})
	}
	if w := cs.getHistoryTurnWarningLocked(currentTurns, prunedTurns); w != "" {
		warnings = append(warnings, warning{Message: w})
	}

	return warnings
}

func (cs *Strategy) getTurnWarningLocked(turn int) string {
	remaining := cs.maxToolTurns - turn
	switch remaining {
	case 3:
		return "[SYSTEM NOTICE: You are approaching the operational turn limit (3 turns remaining). Please begin finalizing your current task and use this turn to perform any final state checks or file reads needed for your summary.]"
	case 2:
		return "[URGENT SYSTEM NOTICE: Only 2 turns remain. You MUST now use 'manage_tasks' to document the distilled state, or use 'manage_history' (pin) to pin critical conversation turns to protect them from pruning. This ensures context efficiency and continuity for the user in future sessions, as conversation history may be pruned.]"
	case 1:
		return "[FINAL SYSTEM WARNING: This is your absolute final turn. You are forbidden from using any more tools. Provide a concise final conclusion or progress summary to the user now. Execution will terminate immediately after this response.]"
	default:
		return ""
	}
}

func (cs *Strategy) getTokenWarningLocked(tokens int) string {
	ratio := float64(tokens) / float64(cs.maxHistoryTokens)
	if ratio > 0.95 {
		return "[CRITICAL SYSTEM NOTICE: Conversation history is at 95% capacity. Immediate risk of session rollback. You must use 'manage_tasks' or 'manage_history' (pin) to save a summary of your work and plans NOW. Keep your response extremely brief.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: The conversation history is at 90% capacity. To avoid a session crash, please minimize large file reads. Use 'manage_tasks' or 'manage_history' (pin) to preserve critical context and architectural notes now, in case a rollback occurs.]"
	}
	return ""
}

func (cs *Strategy) getHistoryTurnWarningLocked(currentTurns, prunedTurns int) string {
	if cs.maxHistoryTurns <= 0 {
		return ""
	}

	if prunedTurns > 5 {
		msg := fmt.Sprintf("[URGENT SYSTEM NOTICE: A major history cleanup has occurred. To maintain performance and cache efficiency, the oldest %d turns of this conversation have been removed. You have lost significant recent context. You MUST refer to 'manage_tasks' to continue unfinished tasks and re-synchronize your internal state.]", prunedTurns)
		return msg
	}

	// Steady-state sliding window: no warning needed for routine single-turn pruning
	if prunedTurns > 0 {
		return ""
	}

	ratio := float64(currentTurns) / float64(cs.maxHistoryTurns)
	if ratio >= 1.0 {
		// Steady state reached. We already warned at 95%.
		return ""
	} else if ratio > 0.95 {
		return "[URGENT SYSTEM NOTICE: Conversation history is at 95% of the turn limit. Pruning is imminent. The oldest messages in this thread will be DELETED after this turn. Move all essential long-term memory to the task list, or pinned via 'manage_history' (pin) immediately.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: Conversation history is at 90% of the turn limit. To prevent loss of context during upcoming pruning, ensure critical architectural decisions and progress are documented in 'manage_tasks', or pinned via 'manage_history' (pin).]"
	}
	return ""
}

// getCloggedWarning returns a warning message for when summarization fails to reduce context.
func (cs *Strategy) getCloggedWarning() string {
	return "[CRITICAL SYSTEM NOTICE: A recent summarization failed to significantly reduce context size. This is likely due to too many 'Pinned' turns or massive active file buffers. You MUST unpin non-essential turns using 'manage_history' (unpin) or move architectural findings to 'manage_tasks' immediately to avoid a session crash.]"
}

func (cs *Strategy) GetContextWindow() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.contextWindow
}

func (cs *Strategy) GetMaxHistoryTokens() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.maxHistoryTokens
}

func (cs *Strategy) GetMaxToolTurns() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.maxToolTurns
}
