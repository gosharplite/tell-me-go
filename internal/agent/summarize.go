// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gosharplite/tell-me-go/internal/types"
)

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

func (a *Agent) summarizeHistory(ctx context.Context, args map[string]interface{}) (string, error) {
	targetTurns, ok := args["turns"].(float64)
	if !ok || targetTurns <= 0 {
		return "", fmt.Errorf("invalid or missing 'turns' parameter")
	}

	msgsToSummarize := int(targetTurns) * 2
	contents := a.history.GetContents()

	// Ensure we don't summarize the current turn (the last message)
	if msgsToSummarize >= len(contents) {
		msgsToSummarize = len(contents) - 1
	}
	if msgsToSummarize <= 0 {
		return "No history to summarize.", nil
	}

	// Adjust to even number to keep turns intact
	if msgsToSummarize%2 != 0 {
		msgsToSummarize--
	}

	if msgsToSummarize <= 0 {
		return "No full turns to summarize.", nil
	}

	summary, err := a.performSummarization(ctx, contents[:msgsToSummarize])
	if err != nil {
		return "", err
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

	if err := a.history.ReplaceRange(0, msgsToSummarize, newMsgs); err != nil {
		return "", fmt.Errorf("failed to update history with summary: %w", err)
	}

	return fmt.Sprintf("Summarized the first %d turns of history.", msgsToSummarize/2), nil
}

func (a *Agent) performSummarization(ctx context.Context, subset []*types.Content) (string, error) {
	a.sm.TerminalLock()
	fmt.Fprintf(os.Stderr, "\033[0;36m[%s] [System] Summarizing %d history entries to free up context...\033[0m\n",
		time.Now().Format("15:04:05"), len(subset))
	a.sm.TerminalUnlock()

	// Construct summarization payload
	summarizerInput := append([]*types.Content{}, subset...)
	summarizerInput = append(summarizerInput, &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: SummarizationPrompt}},
	})

	// Use the same client but with no tools for summarization
	respContent, _, err := a.client.SendChat(summarizerInput, nil)
	if err != nil {
		return "", fmt.Errorf("summarization request failed: %w", err)
	}

	if len(respContent.Parts) == 0 || respContent.Parts[0].Text == "" {
		return "", fmt.Errorf("summarization returned empty content")
	}

	return respContent.Parts[0].Text, nil
}

func (a *Agent) autoSummarize(ctx context.Context) error {
	contents := a.history.GetContents()
	if len(contents) < 10 {
		return fmt.Errorf("not enough history to auto-summarize")
	}

	// Summarize the first 50% of the history
	msgsToSummarize := (len(contents) / 4) * 2 // Roughly 25% for safer margin
	if msgsToSummarize < 2 {
		msgsToSummarize = 2
	}

	summary, err := a.performSummarization(ctx, contents[:msgsToSummarize])
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

	return a.history.ReplaceRange(0, msgsToSummarize, newMsgs)
}
