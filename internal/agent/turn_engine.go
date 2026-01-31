// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// TurnEngine manages the "Think -> Act -> Observe" cycle.
type TurnEngine struct {
	gateway     gateway.LLMGateway
	executor    *ToolExecutor
	history     *history.Manager
	strategy    *ContextStrategy
	renderer    UIRenderer
	registry    ToolRegistry
	logFile     string
	OnTurnStart func()
}

// NewTurnEngine creates a new TurnEngine.
func NewTurnEngine(gw gateway.LLMGateway, ex *ToolExecutor, h *history.Manager, s *ContextStrategy, r UIRenderer, reg ToolRegistry) *TurnEngine {
	return &TurnEngine{
		gateway:  gw,
		executor: ex,
		history:  h,
		strategy: s,
		renderer: r,
		registry: reg,
	}
}

// SetLogFile sets the path for usage logging.
func (e *TurnEngine) SetLogFile(path string) {
	e.logFile = path
}

// Run executes the multi-turn orchestration loop.
func (e *TurnEngine) Run(ctx context.Context, startTime time.Time) error {
	for turn := 0; ; turn++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if e.OnTurnStart != nil {
			e.OnTurnStart()
		}

		_, maxTurns, _ := e.strategy.GetLimits()
		if turn > maxTurns {
			break
		}

		// 1. Prepare Context
		apiContents, tokens, currentTurns, err := e.prepareContext(ctx, turn)
		if err != nil {
			return err
		}

		e.logTurnStatus(currentTurns, tokens, nil, false, startTime)

		// 2. Generate Response
		respContent, metrics, err := e.gateway.Generate(ctx, apiContents, e.registry.GetDeclarations(), e.history.GetResolver())
		if err != nil {
			return err
		}

		// 3. Persist Response
		if err := e.history.AddContent(ctx, respContent); err != nil {
			e.renderer.LogSystemMessage(fmt.Sprintf("Failed to persist history entry: %v", err), "warn")
		}

		// 4. Handle Tool Execution
		if err := e.handleToolExecution(ctx, respContent, turn, metrics); err != nil {
			return err
		}

		e.logTurnStatus(currentTurns, tokens, metrics, true, startTime)

		if metrics != nil {
			e.renderer.LogUsage(metrics, e.logFile, startTime)
		}

		if !e.hasToolCalls(respContent) {
			break
		}
	}
	return nil
}

func (e *TurnEngine) prepareContext(ctx context.Context, turn int) ([]*types.Content, int, int, error) {
	maxTokens, _, maxTurns := e.strategy.GetLimits()

	// 1. Enforce history turn limit
	if maxTurns > 0 {
		pruned := e.history.EnforcePolicy(ctx, history.Policy{MaxTurns: maxTurns})
		if pruned > 0 {
			e.strategy.SetPrunedTurns(pruned)
		}
	}

	contents := e.history.GetContents()
	tokens := e.strategy.EstimateTokens(contents)

	// 2. Auto-Summarization
	if tokens > int(float64(maxTokens)*0.9) {
		if err := e.AutoSummarize(ctx); err == nil {
			contents = e.history.GetContents()
			tokens = e.strategy.EstimateTokens(contents)
		}

		if tokens > maxTokens {
			e.renderer.LogSystemMessage(fmt.Sprintf("Payload estimate (%d tokens) exceeds limit (%d)!", tokens, maxTokens), "error")
			e.renderer.LogSystemMessage("Rolling back history. Please reduce context or start a new session.", "info")
			e.history.Rollback(ctx)
			return nil, 0, 0, ErrContextLimitExceeded
		}
	}

	currentTurns := len(contents) / 2
	warnings := e.strategy.GetWarnings(turn, tokens, currentTurns)

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

		e.renderer.LogSystemMessage("Safety warning injected into volatile model context.", "info")
	}

	return apiContents, tokens, currentTurns, nil
}

func (e *TurnEngine) handleToolExecution(ctx context.Context, respContent *types.Content, turn int, metrics *types.Metrics) error {
	toolStart := time.Now()
	_, maxToolTurns, _ := e.strategy.GetLimits()

	toolResponse, err := e.executor.Execute(ctx, respContent, turn, maxToolTurns)
	if err != nil {
		return err
	}

	if toolResponse != nil {
		if err := e.history.AddContent(ctx, toolResponse); err != nil {
			e.renderer.LogSystemMessage(fmt.Sprintf("Failed to persist history entry: %v", err), "warn")
		}
	}

	if metrics != nil {
		metrics.ToolDuration = time.Since(toolStart).Seconds()
	}
	return nil
}

func (e *TurnEngine) logTurnStatus(currentTurns, tokens int, metrics *types.Metrics, isPost bool, startTime time.Time) {
	maxTokens, _, maxHistTurns := e.strategy.GetLimits()
	e.renderer.LogTurnStatus(TurnStatus{
		Timestamp:        time.Now(),
		CurrentTurns:     currentTurns,
		MaxHistoryTurns:  maxHistTurns,
		Tokens:           tokens,
		MaxHistoryTokens: maxTokens,
		Metrics:          metrics,
		IsPostCall:       isPost,
		StartTime:        startTime,
	})
}

func (e *TurnEngine) hasToolCalls(content *types.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}

// AutoSummarize triggers background compression of older history.
func (e *TurnEngine) AutoSummarize(ctx context.Context) error {
	contents := e.history.GetContents()
	if len(contents) < 10 {
		return fmt.Errorf("not enough history to auto-summarize")
	}

	msgsToSummarize := (len(contents) / 4) * 2
	if msgsToSummarize < 2 {
		msgsToSummarize = 2
	}

	summary, err := e.PerformSummarization(ctx, contents[:msgsToSummarize])
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

	return e.history.ReplaceRange(ctx, 0, msgsToSummarize, newMsgs)
}

// PerformSummarization calls the LLM to compress a subset of history.
func (e *TurnEngine) PerformSummarization(ctx context.Context, subset []*types.Content) (string, error) {
	e.renderer.LogSystemMessage(fmt.Sprintf("Summarizing %d history entries to free up context...", len(subset)), "info")

	summarizerInput := append([]*types.Content{}, subset...)
	summarizerInput = append(summarizerInput, &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: SummarizationPrompt}},
	})

	respContent, _, err := e.gateway.Generate(ctx, summarizerInput, nil, e.history.GetResolver())
	if err != nil {
		return "", fmt.Errorf("summarization request failed: %w", err)
	}

	if len(respContent.Parts) == 0 || respContent.Parts[0].Text == "" {
		return "", fmt.Errorf("summarization returned empty content")
	}

	return respContent.Parts[0].Text, nil
}

// SummarizeHistoryTool implements the summarize_history tool.
func (e *TurnEngine) SummarizeHistoryTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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
	contents := e.history.GetContents()

	if msgsToSummarize >= len(contents) {
		msgsToSummarize = len(contents) - 1
	}
	if msgsToSummarize%2 != 0 {
		msgsToSummarize--
	}
	if msgsToSummarize <= 0 {
		return types.ToolResult{Text: "No history to summarize."}, nil
	}

	summary, err := e.PerformSummarization(ctx, contents[:msgsToSummarize])
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

	if err := e.history.ReplaceRange(ctx, 0, msgsToSummarize, newMsgs); err != nil {
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
