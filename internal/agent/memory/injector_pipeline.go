// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"strings"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// resolveUserPrompt returns the task for the recall query: the last user
// message, which may be empty (passed as-is).
func resolveUserPrompt(req *sessctx.ContextRequest) string {
	return lastUserText(req.History)
}

// fetchEngramPayload issues the per-turn relevance-gated recall query,
// bounded by the inject budget. On a transport error or a server-side
// isError rejection (result.Error — never build a recall block from error
// text, issue #1410) the marker block is stripped in memory only (never
// PersistHistory — no stale recall survives) and (zero, false) is returned.
func (t *plurInjector) fetchEngramPayload(ctx context.Context, req *sessctx.ContextRequest, cfg *config.MemoryConfig, task string) (tools.ToolResult, bool) {
	args := map[string]interface{}{
		"task":   task,
		"budget": cfg.InjectBudget,
	}
	if strings.TrimSpace(cfg.Scope) != "" {
		args["scope"] = cfg.Scope
	}
	result, err := t.client.CallTool(ctx, "plur_inject_hybrid", args)
	if err != nil {
		// Fail-open with strip semantics: strip the marker block in memory
		// only (never PersistHistory — no stale recall survives).
		if t.logger != nil {
			t.logger.Warn("memory_injection_failed", "error", err)
		}
		stripMemoryBlock(req)
		return tools.ToolResult{}, false
	}

	// A server-side isError rejection carries the server's error text in
	// result.Text — never build a recall block from it. Same strip-and-return
	// path as a transport error: inject current recall, or nothing (issue #1410).
	if result.Error != nil {
		if t.logger != nil {
			t.logger.Warn("memory_injection_failed", "error", result.Error)
		}
		stripMemoryBlock(req)
		return tools.ToolResult{}, false
	}

	return result, true
}

// applyMemoryTransformation builds the self-delimited memory block and
// inserts it marker-keyed replace-in-place — never append after first.
// Defensive trim bounds the block to the budget's len/4 heuristic (pinned
// engrams make server-side size non-deterministic), trimming the body to a
// rune-safe byte boundary. An over-budget block is NEVER inserted (site 4 —
// strip-without-insert): a budget too small to hold header+footer degrades
// to no recall rather than an overflowing block.
func (t *plurInjector) applyMemoryTransformation(req *sessctx.ContextRequest, result tools.ToolResult, budget int) bool {
	block := memoryHeader + "\n\n" + strings.TrimSpace(result.Text) + "\n" + memoryFooter

	// Defensive trim: pinned engrams make server-side size non-deterministic.
	// Heuristic tokens = len(block)/4 (matches the model's tokenCount
	// heuristic). Trim the body to a rune-safe byte boundary so the rebuilt
	// block fits the budget.
	if len(block)/4 > budget {
		maxBody := budget*4 - len(memoryHeader) - len(memoryFooter) - 4
		if maxBody < 0 {
			if t.logger != nil {
				t.logger.Debug("memory_block_overflow", "budget", budget, "block_bytes", len(block))
			}
			stripMemoryBlock(req)
			return false
		}
		body := strings.TrimSpace(result.Text)
		if len(body) > maxBody {
			body = truncateToBytes(body, maxBody)
		}
		block = memoryHeader + "\n\n" + body + "\n" + memoryFooter
	}

	// Insert marker-keyed replace-in-place — never append after first.
	insertMemoryBlock(req, block)
	return true
}

// observeInjection records the ADR-068 §8 observability surface on the
// success path: an in-process Warning plus an Info-level log line when
// engram ids were extracted, else a Debug line.
func (t *plurInjector) observeInjection(req *sessctx.ContextRequest, result tools.ToolResult, task string) {
	ids := extractIDs(result)
	if len(ids) > 0 {
		req.Metadata.Warnings = append(req.Metadata.Warnings, "injected_engrams:"+strings.Join(ids, ","))
		if t.logger != nil {
			t.logger.Info("memory_injected", "engrams", strings.Join(ids, ","), "task", task)
		}
	} else if t.logger != nil {
		t.logger.Debug("memory_injected_no_ids", "task", task)
	}
}
