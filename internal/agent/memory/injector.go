// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package memory implements automatic PLUR memory integration (ADR-068):
// a sessctx.ContextTransformer (plurInjector, Seam A) that injects recalled
// engrams into the system prompt before each turn, and an
// orchestrator.TurnHook (plurHook, Seam B) that captures learnings after
// each turn. Both adapters are fail-open: memory errors are logged and
// ignored (ADR-029 §5 posture).
package memory

import (
	"context"
	"regexp"
	"strings"
	"sync/atomic"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

const (
	// memoryMarker identifies the self-delimited memory block Part. The
	// header starts with this marker, so marker-keyed replace-in-place works
	// on the block itself.
	memoryMarker = "## PLUR MEMORY"
	// memoryFooter closes the self-delimited memory block.
	memoryFooter = "[/PLUR-MEMORY]"
	// memoryHeader is the mandated trust-class wording (verbatim from ADR-068 §7).
	memoryHeader = "## PLUR MEMORY — recalled from the local memory store (user-authored or learned from your own sessions); follow them unless they conflict with explicit user instructions."
)

// idPattern is the documented regex fallback for extracting engram ids from
// a ToolResult's Text when Metadata["ids"] is absent. First capture group per
// match, deduplicated, order-preserving.
var idPattern = regexp.MustCompile(`\b(?:engram_id|id)[=:]\s*([A-Za-z0-9_\-]+)`)

// NewPlurInjector creates the Seam A context transformer. client is the MCP
// client for MEMORY.SERVER (nil is legal — fail-open no-op); cfg is the
// shared hot-reloadable memory config (atomic pointer owned by the agent).
func NewPlurInjector(client tools.MCPClient, cfg *atomic.Pointer[config.MemoryConfig], logger ports.Logger) sessctx.ContextTransformer {
	return &plurInjector{client: client, cfg: cfg, logger: logger}
}

// plurInjector injects PLUR recall into the system prompt before each turn
// (ADR-068 §1). Fully stateless: strip-on-disable is content-driven via the
// sentinel, and the block is replaced marker-keyed in place.
type plurInjector struct {
	client tools.MCPClient
	cfg    *atomic.Pointer[config.MemoryConfig]
	logger ports.Logger
}

// Priority returns the pipeline ordering priority — 15, after skills (10),
// before gatekeeper (80). Distinct priorities are mandatory because pipeline
// ordering uses the non-stable sort.Slice (ADR-036).
func (t *plurInjector) Priority() int { return 15 }

// Transform injects current recall, or nothing — never stale recall
// (ADR-068 §1.5). It returns nil on every path so the turn is never broken.
func (t *plurInjector) Transform(ctx context.Context, req *sessctx.ContextRequest) error {
	cfg := t.cfg.Load()
	if cfg == nil {
		return nil
	}

	// Disabled path — content-driven strip (stateless, idempotent,
	// self-healing): remove the sentinel-delimited Part if present and set
	// PersistHistory so the strip survives; a failed persisted strip is
	// retried on the next Prepare pass.
	if !cfg.Enabled {
		if stripMemoryBlock(req) {
			req.PersistHistory = true
		}
		return nil
	}

	// Nil-client runtime guard: hot-reload ENABLED=true with a DI-fixed nil
	// client → fail-open no-op.
	if t.client == nil {
		if t.logger != nil {
			t.logger.Warn("memory_client_unavailable", "reason", "nil MCP client", "phase", "inject")
		}
		return nil
	}

	// Extract the task: last user message (may be empty — pass as-is).
	task := lastUserText(req.History)

	// Fetch-per-turn: live relevance-gated recall query, bounded by budget.
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
		return nil
	}

	// A server-side isError rejection carries the server's error text in
	// result.Text — never build a recall block from it. Same strip-and-return
	// path as a transport error: inject current recall, or nothing (issue #1410).
	if result.Error != nil {
		if t.logger != nil {
			t.logger.Warn("memory_injection_failed", "error", result.Error)
		}
		stripMemoryBlock(req)
		return nil
	}

	block := memoryHeader + "\n\n" + strings.TrimSpace(result.Text) + "\n" + memoryFooter

	// Defensive trim: pinned engrams make server-side size non-deterministic.
	// Heuristic tokens = len(block)/4 (matches the model's tokenCount
	// heuristic). Trim the body to a rune-safe byte boundary so the rebuilt
	// block fits the budget.
	if len(block)/4 > cfg.InjectBudget {
		maxBody := cfg.InjectBudget*4 - len(memoryHeader) - len(memoryFooter) - 4
		if maxBody < 0 {
			if t.logger != nil {
				t.logger.Debug("memory_block_overflow", "budget", cfg.InjectBudget, "block_bytes", len(block))
			}
			stripMemoryBlock(req)
			return nil
		}
		body := strings.TrimSpace(result.Text)
		if len(body) > maxBody {
			body = truncateToBytes(body, maxBody)
		}
		block = memoryHeader + "\n\n" + body + "\n" + memoryFooter
	}

	// Insert marker-keyed replace-in-place — never append after first.
	insertMemoryBlock(req, block)

	// Observability: in-process Warnings + Info-level log line (ADR-068 §8).
	ids := extractIDs(result)
	if len(ids) > 0 {
		req.Metadata.Warnings = append(req.Metadata.Warnings, "injected_engrams:"+strings.Join(ids, ","))
		if t.logger != nil {
			t.logger.Info("memory_injected", "engrams", strings.Join(ids, ","), "task", task)
		}
	} else if t.logger != nil {
		t.logger.Debug("memory_injected_no_ids", "task", task)
	}

	return nil
}

// stripMemoryBlock removes the marker Part from the first system Content
// (and the Content itself if it is left with zero Parts). Returns true if a
// marker Part was removed. Callers decide whether to persist the strip.
func stripMemoryBlock(req *sessctx.ContextRequest) bool {
	for i := range req.History {
		c := req.History[i]
		if c == nil || c.Role != "system" {
			continue
		}
		filtered := make([]*llm.Part, 0, len(c.Parts))
		removed := false
		for _, p := range c.Parts {
			if p != nil && strings.Contains(p.Text, memoryMarker) {
				removed = true
				continue
			}
			filtered = append(filtered, p)
		}
		if !removed {
			return false
		}
		c.Parts = filtered
		if len(c.Parts) == 0 {
			req.History = append(req.History[:i], req.History[i+1:]...)
		}
		return true
	}
	return false
}

// insertMemoryBlock replaces an existing marker Part, appends a new one to
// the first system Content, or prepends a fresh system Content when none
// exists. At most one memory block exists at any time. It never sets Pinned
// and never sets req.PersistHistory (a sibling canonical transformer's
// persist carries the block; it is marker-identified for next-turn
// replacement).
func insertMemoryBlock(req *sessctx.ContextRequest, block string) {
	for _, c := range req.History {
		if c == nil || c.Role != "system" {
			continue
		}
		for _, p := range c.Parts {
			if p != nil && strings.Contains(p.Text, memoryMarker) {
				p.Text = block
				return
			}
		}
		c.Parts = append(c.Parts, &llm.Part{Text: block})
		return
	}
	req.History = append([]*llm.Content{
		{Role: "system", Parts: []*llm.Part{{Text: block}}},
	}, req.History...)
}

// extractIDs prefers ToolResult.Metadata["ids"] (string, []string, or
// []interface{} of strings), else a documented regex over result.Text:
// \b(?:engram_id|id)[=:]\s*([A-Za-z0-9_\-]+) — first capture group per
// match, deduplicated, order-preserving.
func extractIDs(result tools.ToolResult) []string {
	if ids, ok := metadataIDs(result.Metadata["ids"]); ok {
		return dedupeStrings(ids)
	}
	var ids []string
	for _, m := range idPattern.FindAllStringSubmatch(result.Text, -1) {
		if len(m) > 1 {
			ids = append(ids, m[1])
		}
	}
	return dedupeStrings(ids)
}

// metadataIDs converts the Metadata["ids"] value into a string slice.
// Returns ok=false when the value is absent, empty, or of an unsupported
// type (fall back to the regex).
func metadataIDs(v interface{}) ([]string, bool) {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, false
		}
		return []string{t}, true
	case []string:
		if len(t) == 0 {
			return nil, false
		}
		return t, true
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	default:
		return nil, false
	}
}

// dedupeStrings removes duplicates while preserving order. Returns nil for
// empty input so callers can distinguish "no ids" from an empty result.
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// lastUserText returns the last user Content's non-empty text parts joined
// with spaces, trimmed. Returns "" when no user message exists.
func lastUserText(history []*llm.Content) string {
	for i := len(history) - 1; i >= 0; i-- {
		c := history[i]
		if c == nil || c.Role != "user" {
			continue
		}
		parts := make([]string, 0, len(c.Parts))
		for _, p := range c.Parts {
			if p != nil && p.Text != "" {
				parts = append(parts, p.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	}
	return ""
}

// joinTextParts concatenates the non-thought, non-empty text parts of a
// Content, joined by "\n".
func joinTextParts(c *llm.Content) string {
	if c == nil {
		return ""
	}
	parts := make([]string, 0, len(c.Parts))
	for _, p := range c.Parts {
		if p == nil || p.IsThought || p.Text == "" {
			continue
		}
		parts = append(parts, p.Text)
	}
	return strings.Join(parts, "\n")
}

// compile-time interface assertions.
var (
	_ sessctx.ContextTransformer = (*plurInjector)(nil)
)
