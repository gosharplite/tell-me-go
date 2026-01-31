// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// ContextManager encapsulates context preparation, policy enforcement, and summarization.
type ContextManager struct {
	Strategy *ContextStrategy
	History  *history.Manager
	Gateway  gateway.LLMGateway
	Renderer UIRenderer
}

// NewContextManager creates a new ContextManager.
func NewContextManager(s *ContextStrategy, h *history.Manager, g gateway.LLMGateway, r UIRenderer) *ContextManager {
	return &ContextManager{Strategy: s, History: h, Gateway: g, Renderer: r}
}

// Prepare calculates the current context, enforces limits, and handles auto-summarization.
func (cm *ContextManager) Prepare(ctx context.Context, turn int) ([]*types.Content, int, int, error) {
	maxTokens, _, maxTurns := cm.Strategy.GetLimits()

	// 1. Enforce history turn limit
	if maxTurns > 0 {
		pruned := cm.History.EnforcePolicy(ctx, history.Policy{MaxTurns: maxTurns})
		if pruned > 0 {
			cm.Strategy.SetPrunedTurns(pruned)
		}
	}

	contents := cm.History.GetContents()
	tokens := cm.Strategy.EstimateTokens(contents)

	// 2. Auto-Summarization
	if tokens > int(float64(maxTokens)*0.9) {
		if err := cm.AutoSummarize(ctx); err == nil {
			contents = cm.History.GetContents()
			tokens = cm.Strategy.EstimateTokens(contents)
		}

		if tokens > maxTokens {
			cm.Renderer.LogSystemMessage(fmt.Sprintf("Payload estimate (%d tokens) exceeds limit (%d)!", tokens, maxTokens), "error")
			cm.Renderer.LogSystemMessage("Rolling back history. Please reduce context or start a new session.", "info")
			cm.History.Rollback(ctx)
			return nil, 0, 0, ErrContextLimitExceeded
		}
	}

	currentTurns := len(contents) / 2
	warnings := cm.Strategy.GetWarnings(turn, tokens, currentTurns)

	apiContents := make([]*types.Content, len(contents))
	copy(apiContents, contents)

	if len(warnings) > 0 && len(apiContents) > 0 {
		var combined string
		for _, w := range warnings {
			if combined != "" {
				combined += "\n"
			}
			combined += w.Message
		}

		lastIdx := len(apiContents) - 1
		orig := apiContents[lastIdx]
		cloned := &types.Content{
			Role:  orig.Role,
			Parts: make([]*types.Part, len(orig.Parts)),
		}
		copy(cloned.Parts, orig.Parts)
		cloned.Parts = append(cloned.Parts, &types.Part{
			Text: "\n\n" + combined,
		})
		apiContents[lastIdx] = cloned

		cm.Renderer.LogSystemMessage("Safety warning injected into volatile model context.", "info")
	}

	return apiContents, tokens, currentTurns, nil
}

// AutoSummarize triggers background compression of older history.
func (cm *ContextManager) AutoSummarize(ctx context.Context) error {
	contents := cm.History.GetContents()
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

	return cm.History.ReplaceRange(ctx, 0, msgsToSummarize, newMsgs)
}

// PerformSummarization calls the LLM to compress a subset of history.
func (cm *ContextManager) PerformSummarization(ctx context.Context, subset []*types.Content) (string, error) {
	cm.Renderer.LogSystemMessage(fmt.Sprintf("Summarizing %d history entries to free up context...", len(subset)), "info")

	summarizerInput := append([]*types.Content{}, subset...)
	summarizerInput = append(summarizerInput, &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: SummarizationPrompt}},
	})

	respCh, finalize := cm.Gateway.Generate(ctx, summarizerInput, nil, cm.History.GetResolver())
	// Drain the channel; we don't stream summarization to the UI.
	for range respCh {
	}
	respContent, _, err := finalize()
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
	if err := types.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	targetTurns := int(params.Turns)
	if targetTurns <= 0 {
		return types.ToolResult{}, fmt.Errorf("invalid 'turns' parameter")
	}

	msgsToSummarize := targetTurns * 2
	contents := cm.History.GetContents()

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

	if err := cm.History.ReplaceRange(ctx, 0, msgsToSummarize, newMsgs); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to update history with summary: %w", err)
	}

	return types.ToolResult{Text: fmt.Sprintf("Summarized the first %d turns of history.", targetTurns)}, nil
}

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
