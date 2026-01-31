// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// SummarizationPrompt is the system instruction for history compression.
const SummarizationPrompt = `You are a conversation compressor. Summarize the provided history into a concise but comprehensive state summary.
Preserve:
1. Current architecture decisions and project structure.
2. Modified files and their high-level changes.
3. Successfully executed commands and their critical results.
4. Unresolved issues or pending tasks from the scratchpad/task list.
Discard:
1. Large file contents or boilerplate code output.
2. Redundant tool call logs.
3. "Trial and error" failures that don't affect the final state.

The output must be a single summary that will replace these turns in the history.
`

// ContextManager handles token estimation, history pruning, and summarization.
type ContextManager struct {
	client           types.LLMClient
	history          *history.Manager
	registry         *tools.Registry
	sm               *tools.SecurityManager
	maxHistoryTokens int
	maxToolTurns     int
	maxHistoryTurns  int
	prunedTurns      int
	smartSuggestions bool
}

// NewContextManager creates a new context manager.
func NewContextManager(client types.LLMClient, history *history.Manager, registry *tools.Registry, sm *tools.SecurityManager) *ContextManager {
	return &ContextManager{
		client:           client,
		history:          history,
		registry:         registry,
		sm:               sm,
		maxHistoryTokens: 120000,
		maxToolTurns:     10,
		maxHistoryTurns:  20,
		smartSuggestions: false,
	}
}

// SetSmartSuggestions sets the smart suggestions preference.
func (cm *ContextManager) SetSmartSuggestions(enabled bool) {
	cm.smartSuggestions = enabled
}

// SetLimits updates the operational limits.
func (cm *ContextManager) SetLimits(historyTokens, toolTurns, historyTurns int) {
	if historyTokens > 0 {
		cm.maxHistoryTokens = historyTokens
	}
	if toolTurns > 0 {
		cm.maxToolTurns = toolTurns
	}
	if historyTurns > 0 {
		cm.maxHistoryTurns = historyTurns
	}
}

// SetPrunedTurns sets the initial pruned turns count.
func (cm *ContextManager) SetPrunedTurns(n int) {
	cm.prunedTurns = n
}

// GetPrunedTurns returns the current pruned turns count.
func (cm *ContextManager) GetPrunedTurns() int {
	return cm.prunedTurns
}

// GetLimits returns the current limits.
func (cm *ContextManager) GetLimits() (int, int, int) {
	return cm.maxHistoryTokens, cm.maxToolTurns, cm.maxHistoryTurns
}

// PrepareContents ensures the history fits within limits and includes safety warnings.
func (cm *ContextManager) PrepareContents(ctx context.Context, turn int) ([]*types.Content, int, int, error) {
	contents := cm.history.GetContents()

	// 1. Enforce history turn limit
	if cm.maxHistoryTurns > 0 && len(contents) > cm.maxHistoryTurns*2 {
		pruned, newContents := cm.history.Prune(cm.maxHistoryTurns)
		if pruned > 0 {
			cm.prunedTurns += pruned
			contents = newContents
		}
	}

	tokens := cm.EstimateTokens(contents)

	// 2. Safety Check: MAX_HISTORY_TOKENS
	if tokens > int(float64(cm.maxHistoryTokens)*0.9) {
		if err := cm.AutoSummarize(ctx); err == nil {
			contents = cm.history.GetContents()
			tokens = cm.EstimateTokens(contents)
		}

		if tokens > cm.maxHistoryTokens {
			cm.handleLimitExceeded(tokens)
			return nil, 0, 0, ErrContextLimitExceeded
		}
	}

	currentTurns := len(contents) / 2
	apiContents := cm.injectWarnings(contents, turn, tokens, currentTurns)

	return apiContents, tokens, currentTurns, nil
}

// EstimateTokens provides a heuristic-based token count.
func (cm *ContextManager) EstimateTokens(contents []*types.Content) int {
	charCount := 0
	for _, decl := range cm.registry.GetDeclarations() {
		charCount += len(decl.Name) + len(decl.Description)
		if decl.Parameters != nil {
			charCount += 200 // Heuristic for parameter definitions
		}
	}
	for _, c := range contents {
		for _, p := range c.Parts {
			if p.Text != "" {
				charCount += len(p.Text)
			}
			if p.FunctionCall != nil {
				charCount += len(p.FunctionCall.Name)
				charCount += cm.estimateMapSize(p.FunctionCall.Args)
			}
			if p.FunctionResponse != nil {
				charCount += len(p.FunctionResponse.Name)
				charCount += cm.estimateMapSize(p.FunctionResponse.Response)
			}
			if p.InlineData != nil {
				charCount += 50 // Minimal tokens for blob reference
			}
		}
	}
	charCount += 1000 // Base overhead
	return int(float64(charCount) / 3.2)
}

func (cm *ContextManager) estimateMapSize(m map[string]interface{}) int {
	if m == nil {
		return 0
	}
	size := 0
	for k, v := range m {
		size += len(k)
		size += cm.estimateValueSize(v)
	}
	return size
}

func (cm *ContextManager) estimateValueSize(v interface{}) int {
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
		return cm.estimateMapSize(val)
	case []interface{}:
		size := 0
		for _, item := range val {
			size += cm.estimateValueSize(item)
		}
		return size
	default:
		return 20
	}
}

func (cm *ContextManager) injectWarnings(contents []*types.Content, turn, tokens, currentTurns int) []*types.Content {
	apiContents := make([]*types.Content, len(contents))
	copy(apiContents, contents)

	warning := cm.getTurnWarning(turn)
	if tokenWarning := cm.getTokenWarning(tokens); tokenWarning != "" {
		if warning != "" {
			warning += "\n" + tokenWarning
		} else {
			warning = tokenWarning
		}
	}
	if turnWarning := cm.getHistoryTurnWarning(currentTurns); turnWarning != "" {
		if warning != "" {
			warning += "\n" + turnWarning
		} else {
			warning = turnWarning
		}
	}
	if cm.smartSuggestions {
		suggestionInstr := "[UX PREFERENCE: smart_suggestions is ENABLED. You MUST conclude every final response (when no more tools are needed) by suggesting 2 to 3 context-aware follow-up commands (tool calls or workflow actions) relevant to the current conversation state. Format them as a clear bulleted list at the very end of your message.]"
		if warning != "" {
			warning += "\n" + suggestionInstr
		} else {
			warning = suggestionInstr
		}
	}

	if warning != "" && len(apiContents) > 0 {
		lastIdx := len(apiContents) - 1
		orig := apiContents[lastIdx]
		cloned := &types.Content{
			Role:  orig.Role,
			Parts: make([]*types.Part, len(orig.Parts)),
		}
		copy(cloned.Parts, orig.Parts)
		cloned.Parts = append(cloned.Parts, &types.Part{
			Text: "\n\n" + warning,
		})
		apiContents[lastIdx] = cloned

		func() {
			cm.sm.TerminalLock()
			defer cm.sm.TerminalUnlock()
			fmt.Fprintf(os.Stderr, "\033[0;33m[%s] [System] Safety warning injected into volatile model context.\033[0m\n",
				time.Now().Format("15:04:05"))
		}()
	}
	return apiContents
}

func (cm *ContextManager) getTurnWarning(turn int) string {
	remaining := cm.maxToolTurns - turn
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

func (cm *ContextManager) getTokenWarning(tokens int) string {
	ratio := float64(tokens) / float64(cm.maxHistoryTokens)
	if ratio > 0.95 {
		return "[CRITICAL SYSTEM NOTICE: Conversation history is at 95% capacity. Immediate risk of session rollback. You must use 'manage_scratchpad' and 'manage_tasks' to save a summary of your work and plans NOW. Keep your response extremely brief.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: The conversation history is at 90% capacity. To avoid a session crash, please minimize large file reads. Use 'manage_scratchpad' and 'manage_tasks' to save your current progress and architectural notes now, in case a rollback occurs.]"
	}
	return ""
}

func (cm *ContextManager) getHistoryTurnWarning(currentTurns int) string {
	if cm.maxHistoryTurns <= 0 {
		return ""
	}

	if cm.prunedTurns > 5 {
		msg := fmt.Sprintf("[URGENT SYSTEM NOTICE: A major history cleanup has occurred. To maintain performance and cache efficiency, the oldest %d turns of this conversation have been removed. You have lost significant recent context. You MUST refer to the 'manage_scratchpad' and read 'manage_tasks' to continue unfinished tasks and re-synchronize your internal state.]", cm.prunedTurns)
		cm.prunedTurns = 0
		return msg
	}

	ratio := float64(currentTurns) / float64(cm.maxHistoryTurns)
	if ratio >= 1.0 {
		return "[SYSTEM NOTICE: The history turn limit has been reached and the oldest messages in this conversation have been deleted. If you are missing previous context or architectural details, please refer to 'manage_scratchpad' and 'manage_tasks' for the latest status and pending tasks.]"
	} else if ratio > 0.95 {
		return "[URGENT SYSTEM NOTICE: Conversation history is at 95% of the turn limit. Pruning is imminent. The oldest messages in this thread will be DELETED after this turn. Move all essential long-term memory to the scratchpad and task list immediately.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: Conversation history is at 90% of the turn limit. To prevent loss of context during upcoming pruning, ensure critical architectural decisions and progress are documented in the scratchpad and 'manage_tasks'.]"
	}
	return ""
}

func (cm *ContextManager) handleLimitExceeded(tokens int) {
	cm.sm.TerminalLock()
	defer cm.sm.TerminalUnlock()

	fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Safety Error] Payload estimate (%d tokens) exceeds limit (%d)!\033[0m\n",
		time.Now().Format("15:04:05"), tokens, cm.maxHistoryTokens)
	fmt.Fprintf(os.Stderr, "\033[0;33m[%s] [System] Rolling back history. Please reduce context or start a new session.\033[0m\n",
		time.Now().Format("15:04:05"))
	cm.history.Rollback()
}

// AutoSummarize triggers background compression of older history.
func (cm *ContextManager) AutoSummarize(ctx context.Context) error {
	contents := cm.history.GetContents()
	if len(contents) < 10 {
		return fmt.Errorf("not enough history to auto-summarize")
	}

	msgsToSummarize := (len(contents) / 4) * 2
	if msgsToSummarize < 2 {
		msgsToSummarize = 2
	}

	summary, err := cm.PerformSummarization(ctx, contents[:msgsToSummarize])
	if err != nil {
		return err
	}

	newMsgs := []*types.Content{
		{
			Role:  "user",
			Parts: []*types.Part{{Text: "System Auto-Summary (context limit reached):\n\n" + summary}},
		},
		{
			Role:  "model",
			Parts: []*types.Part{{Text: "Understood. Context compressed."}},
		},
	}

	return cm.history.ReplaceRange(0, msgsToSummarize, newMsgs)
}

// PerformSummarization calls the LLM to compress a subset of history.
func (cm *ContextManager) PerformSummarization(ctx context.Context, subset []*types.Content) (string, error) {
	func() {
		cm.sm.TerminalLock()
		defer cm.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "\033[0;36m[%s] [System] Summarizing %d history entries to free up context...\033[0m\n",
			time.Now().Format("15:04:05"), len(subset))
	}()

	summarizerInput := append([]*types.Content{}, subset...)
	summarizerInput = append(summarizerInput, &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: SummarizationPrompt}},
	})

	respContent, _, err := cm.client.SendChat(ctx, summarizerInput, nil, cm.history.GetResolver())
	if err != nil {
		return "", fmt.Errorf("summarization request failed: %w", err)
	}

	if len(respContent.Parts) == 0 || respContent.Parts[0].Text == "" {
		return "", fmt.Errorf("summarization returned empty content")
	}

	return respContent.Parts[0].Text, nil
}

// SummarizeHistoryTool implements the summarize_history tool.
func (cm *ContextManager) SummarizeHistoryTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Turns float64 `json:"turns"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	targetTurns := int(params.Turns)
	if targetTurns <= 0 {
		return types.ToolResult{}, fmt.Errorf("invalid 'turns' parameter")
	}

	msgsToSummarize := targetTurns * 2
	contents := cm.history.GetContents()

	if msgsToSummarize >= len(contents) {
		msgsToSummarize = len(contents) - 1
	}
	if msgsToSummarize%2 != 0 {
		msgsToSummarize--
	}
	if msgsToSummarize <= 0 {
		return types.ToolResult{Text: "No history to summarize."}, nil
	}

	summary, err := cm.PerformSummarization(ctx, contents[:msgsToSummarize])
	if err != nil {
		return types.ToolResult{}, err
	}

	newMsgs := []*types.Content{
		{
			Role:  "user",
			Parts: []*types.Part{{Text: "System Summary of previous context:\n\n" + summary}},
		},
		{
			Role:  "model",
			Parts: []*types.Part{{Text: "Understood. I have integrated the summarized context."}},
		},
	}

	if err := cm.history.ReplaceRange(0, msgsToSummarize, newMsgs); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to update history with summary: %w", err)
	}

	return types.ToolResult{Text: fmt.Sprintf("Summarized the first %d turns of history.", targetTurns)}, nil
}
