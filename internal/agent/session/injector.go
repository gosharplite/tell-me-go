// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// WarningInjector adds safety warnings to the context.
type WarningInjector struct {
	Strategy *ContextStrategy
}

func (t *WarningInjector) Transform(ctx context.Context, req *ports.ContextRequest) error {
	tokens := req.Metadata.FinalTokenCount
	// Count logical conversational turns by counting messages with RoleUser.
	// This prevents tool-call loops from artificially inflating the turn count.
	currentTurns := 0
	for _, msg := range req.History {
		if msg.Role == "user" {
			// A true conversational user turn does not have a FunctionResponse
			isToolResponse := false
			for _, p := range msg.Parts {
				if p.FunctionResponse != nil {
					isToolResponse = true
					break
				}
			}
			if !isToolResponse {
				currentTurns++
			}
		}
	}

	combined, list := t.gatherWarnings(req, tokens, currentTurns)
	if combined == "" {
		return nil
	}

	// Move side-effect here
	req.Metadata.Warnings = append(req.Metadata.Warnings, list...)

	t.injectWarning(req, combined)
	return nil
}

func (t *WarningInjector) gatherWarnings(req *ports.ContextRequest, tokens, turns int) (string, []string) {
	var combined string
	var list []string
	maxTokens, _, _ := t.Strategy.getLimits()

	// Prioritize the Clogged warning if maintenance failed to reduce size OR was blocked by pins,
	// and we are still near capacity.
	if (req.Metadata.SummarizationAttempted || req.Metadata.MaintenanceBlocked) && float64(tokens) > float64(maxTokens)*0.85 {
		combined = t.Strategy.getCloggedWarning()
		list = append(list, combined)
	} else {
		warnings := t.Strategy.getWarnings(req.Turn, tokens, turns, req.Metadata.PrunedTurns)
		if len(warnings) == 0 {
			return "", nil
		}
		for _, w := range warnings {
			if combined != "" {
				combined += "\n"
			}
			combined += w.Message
			list = append(list, w.Message)
		}
	}
	return combined, list
}

func (t *WarningInjector) injectWarning(req *ports.ContextRequest, combined string) {
	if len(req.History) == 0 {
		return
	}

	// [IDEMPOTENCY CHECK]
	// Look back at the last 2 complete turns (approx 4 messages)
	lookback := 4
	if len(req.History) < lookback {
		lookback = len(req.History)
	}

	for i := len(req.History) - 1; i >= len(req.History)-lookback; i-- {
		msg := req.History[i]
		for _, p := range msg.Parts {
			if strings.Contains(p.Text, combined) {
				// Warning already exists in recent history, skip injection
				return
			}
		}
		// Also check TransientParts in case they haven't been merged yet
		for _, p := range msg.TransientParts {
			if strings.Contains(p.Text, combined) {
				return
			}
		}
	}

	lastIdx := len(req.History) - 1
	orig := req.History[lastIdx]

	cloned := llm.CloneContent(orig)
	cloned.TransientParts = append(cloned.TransientParts, &llm.Part{
		Text: "\n\n" + combined,
	})
	req.History[lastIdx] = cloned
}

func (t *WarningInjector) Priority() int { return priorityTransientThreshold }
