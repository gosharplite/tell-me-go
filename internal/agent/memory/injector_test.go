// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// TestInjectorDisabledStrip covers the content-driven strip-on-disable path:
// no marker → no-op (PersistHistory unchanged, no MCP call); marker Part →
// removed with PersistHistory set; zero-Parts system Content → dropped.
func TestInjectorDisabledStrip(t *testing.T) {
	markerBlock := memoryHeader + "\n\nrecall\n" + memoryFooter
	tests := []struct {
		name           string
		history        []*llm.Content
		wantPersist    bool
		wantHistoryLen int
		wantRoleFirst  string
	}{
		{
			name: "no marker no-op",
			history: []*llm.Content{
				{Role: "system", Parts: []*llm.Part{{Text: "base"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "hi"}}},
			},
			wantPersist:    false,
			wantHistoryLen: 2,
			wantRoleFirst:  "system",
		},
		{
			name: "marker part removed other kept",
			history: []*llm.Content{
				{Role: "system", Parts: []*llm.Part{{Text: "base"}, {Text: markerBlock}}},
				{Role: "user", Parts: []*llm.Part{{Text: "hi"}}},
			},
			wantPersist:    true,
			wantHistoryLen: 2,
			wantRoleFirst:  "system",
		},
		{
			name: "system content dropped when zero parts",
			history: []*llm.Content{
				{Role: "system", Parts: []*llm.Part{{Text: markerBlock}}},
				{Role: "user", Parts: []*llm.Part{{Text: "hi"}}},
			},
			wantPersist:    true,
			wantHistoryLen: 1,
			wantRoleFirst:  "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMCPClient{}
			inj, _ := newTestInjector(t, mock, false)
			req := &sessctx.ContextRequest{History: tt.history}
			if err := inj.Transform(context.Background(), req); err != nil {
				t.Fatalf("Transform error: %v", err)
			}
			if req.PersistHistory != tt.wantPersist {
				t.Errorf("PersistHistory = %v, want %v", req.PersistHistory, tt.wantPersist)
			}
			if len(req.History) != tt.wantHistoryLen {
				t.Errorf("history len = %d, want %d", len(req.History), tt.wantHistoryLen)
			}
			if len(req.History) > 0 && req.History[0].Role != tt.wantRoleFirst {
				t.Errorf("first content role = %q, want %q", req.History[0].Role, tt.wantRoleFirst)
			}
			if countMarkerParts(req.History) != 0 {
				t.Error("marker still present after strip")
			}
			if calls := mock.recordedNames(); len(calls) != 0 {
				t.Errorf("disabled path must not call MCP, got %v", calls)
			}
		})
	}
}

// TestInjectorNilCfg verifies the nil-config guard: no-op, no panic.
func TestInjectorNilCfg(t *testing.T) {
	cfgPtr := &atomic.Pointer[config.MemoryConfig]{}
	cfgPtr.Store(nil)
	inj := &plurInjector{client: &mockMCPClient{}, cfg: cfgPtr, logger: &ports.NoOpLogger{}}
	req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
	if err := inj.Transform(context.Background(), req); err != nil {
		t.Fatalf("nil cfg must no-op, got err %v", err)
	}
	if len(req.History) != 1 {
		t.Errorf("history mutated with nil cfg: %d entries", len(req.History))
	}
}

// TestInjectorEnabledInsert covers the enabled-path insertion behaviors:
// prepend when no system Content, append to an existing system Content,
// replace an existing marker Part — at most one memory block.
func TestInjectorEnabledInsert(t *testing.T) {
	t.Run("no system content prepends", func(t *testing.T) {
		mock := &mockMCPClient{CallToolFunc: recall("recall text")}
		inj, _ := newTestInjector(t, mock, true)
		req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform error: %v", err)
		}
		if len(req.History) != 2 || req.History[0].Role != "system" {
			t.Fatalf("expected prepended system Content, got %d entries", len(req.History))
		}
		sys := req.History[0]
		if sys.Pinned {
			t.Error("injected block must not be pinned")
		}
		if len(sys.Parts) != 1 {
			t.Fatalf("expected exactly one part, got %d", len(sys.Parts))
		}
		text := sys.Parts[0].Text
		if !strings.Contains(text, memoryHeader) || !strings.Contains(text, "recall text") || !strings.Contains(text, memoryFooter) {
			t.Errorf("block missing components: %q", text)
		}
		if req.PersistHistory {
			t.Error("PersistHistory must remain unchanged on the enabled path")
		}
		if calls := mock.recordedNames(); len(calls) != 1 || calls[0] != "CallTool:plur_inject_hybrid" {
			t.Errorf("unexpected MCP calls: %v", calls)
		}
	})

	t.Run("appends part to existing system", func(t *testing.T) {
		mock := &mockMCPClient{CallToolFunc: recall("recall text")}
		inj, _ := newTestInjector(t, mock, true)
		req := &sessctx.ContextRequest{History: []*llm.Content{
			{Role: "system", Parts: []*llm.Part{{Text: "base"}}},
			{Role: "user", Parts: []*llm.Part{{Text: "hi"}}},
		}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform error: %v", err)
		}
		sys := req.History[0]
		if len(sys.Parts) != 2 {
			t.Fatalf("expected 2 parts (base + block), got %d", len(sys.Parts))
		}
		if sys.Parts[0].Text != "base" {
			t.Errorf("first part = %q, want %q", sys.Parts[0].Text, "base")
		}
		if !strings.Contains(sys.Parts[1].Text, memoryHeader) {
			t.Errorf("second part is not the memory block: %q", sys.Parts[1].Text)
		}
		if countMarkerParts(req.History) != 1 {
			t.Errorf("expected exactly one marker part (memory-single-block)")
		}
	})

	t.Run("replaces existing marker part", func(t *testing.T) {
		mock := &mockMCPClient{CallToolFunc: recall("new recall")}
		inj, _ := newTestInjector(t, mock, true)
		req := &sessctx.ContextRequest{History: []*llm.Content{
			{Role: "system", Parts: []*llm.Part{{Text: "base"}, {Text: memoryHeader + "\n\nstale recall\n" + memoryFooter}}},
			{Role: "user", Parts: []*llm.Part{{Text: "hi"}}},
		}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform error: %v", err)
		}
		sys := req.History[0]
		if len(sys.Parts) != 2 {
			t.Fatalf("expected 2 parts, got %d", len(sys.Parts))
		}
		if !strings.Contains(sys.Parts[1].Text, "new recall") || strings.Contains(sys.Parts[1].Text, "stale recall") {
			t.Errorf("marker part not replaced in place: %q", sys.Parts[1].Text)
		}
		if countMarkerParts(req.History) != 1 {
			t.Errorf("expected exactly one marker part (memory-single-block)")
		}
	})
}

// TestInjectorFailOpenStrip verifies the fail-open contract: on an MCP error,
// the marker block is stripped in memory only — PersistHistory is NOT set —
// and Transform returns nil.
func TestInjectorFailOpenStrip(t *testing.T) {
	mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, errors.New("inject failed")
	}}
	inj, _ := newTestInjector(t, mock, true)
	req := &sessctx.ContextRequest{History: []*llm.Content{
		{Role: "system", Parts: []*llm.Part{{Text: "base"}, {Text: memoryHeader + "\n\nstale\n" + memoryFooter}}},
		{Role: "user", Parts: []*llm.Part{{Text: "hi"}}},
	}}
	if err := inj.Transform(context.Background(), req); err != nil {
		t.Fatalf("Transform must return nil on failure, got %v", err)
	}
	if req.PersistHistory {
		t.Error("PersistHistory must NOT be set on the fail-open path")
	}
	sys := req.History[0]
	if len(sys.Parts) != 1 || strings.Contains(sys.Parts[0].Text, memoryMarker) {
		t.Errorf("marker not stripped in memory: %+v", sys.Parts)
	}
	if calls := mock.recordedNames(); len(calls) != 1 || calls[0] != "CallTool:plur_inject_hybrid" {
		t.Errorf("unexpected MCP calls: %v", calls)
	}
}

// TestInjectorNilClient verifies the nil-client runtime guard: no-op, nil
// error, no panic, history untouched.
func TestInjectorNilClient(t *testing.T) {
	inj, _ := newTestInjector(t, nil, true)
	req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
	if err := inj.Transform(context.Background(), req); err != nil {
		t.Fatalf("nil client must no-op, got err %v", err)
	}
	if len(req.History) != 1 {
		t.Errorf("history mutated with nil client: %d entries", len(req.History))
	}
	if req.PersistHistory {
		t.Error("PersistHistory must remain unchanged")
	}
}

// TestInjectorTrim verifies the defensive trim: a very long recall results
// in a block whose len/4 heuristic fits the inject budget.
func TestInjectorTrim(t *testing.T) {
	long := strings.Repeat("lorem ipsum dolor sit amet ", 20000)
	mock := &mockMCPClient{CallToolFunc: recall(long)}
	inj, cfgPtr := newTestInjector(t, mock, true)
	req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
	if err := inj.Transform(context.Background(), req); err != nil {
		t.Fatalf("Transform error: %v", err)
	}
	cfg := cfgPtr.Load()
	block := req.History[0].Parts[0].Text
	if got := len(block) / 4; got > cfg.InjectBudget {
		t.Errorf("block len/4 = %d exceeds budget %d", got, cfg.InjectBudget)
	}
	if !strings.HasPrefix(block, memoryHeader) || !strings.HasSuffix(block, memoryFooter) {
		t.Errorf("trimmed block lost header/footer: %q", block)
	}
	if countMarkerParts(req.History) != 1 {
		t.Errorf("expected exactly one marker part after trim")
	}
}

// TestInjectorTrimOverflow verifies the maxBody < 0 overflow path: the block
// is stripped in memory and Transform returns nil.
func TestInjectorTrimOverflow(t *testing.T) {
	mock := &mockMCPClient{CallToolFunc: recall("recall")}
	inj, cfgPtr := newTestInjector(t, mock, true)
	memCfg := cfgPtr.Load()
	memCfg.InjectBudget = 0 // header+footer alone exceed the budget
	cfgPtr.Store(memCfg)
	req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
	if err := inj.Transform(context.Background(), req); err != nil {
		t.Fatalf("Transform must return nil on overflow, got %v", err)
	}
	if countMarkerParts(req.History) != 0 {
		t.Error("marker must be stripped on overflow")
	}
	if req.PersistHistory {
		t.Error("PersistHistory must not be set on overflow")
	}
}

// TestInjectorWarnings verifies the observability surface: metadata ids and
// the regex fallback both produce injected_engrams:<ids> warnings.
func TestInjectorWarnings(t *testing.T) {
	t.Run("metadata ids", func(t *testing.T) {
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "recall", Metadata: map[string]interface{}{"ids": []string{"e1", "e2"}}}, nil
		}}
		inj, _ := newTestInjector(t, mock, true)
		req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform error: %v", err)
		}
		if !containsString(req.Metadata.Warnings, "injected_engrams:e1,e2") {
			t.Errorf("warnings = %v, want injected_engrams:e1,e2", req.Metadata.Warnings)
		}
	})

	t.Run("regex fallback deduplicated", func(t *testing.T) {
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "engram_id: e1\nid: e2\nid: e1\nid: e3"}, nil
		}}
		inj, _ := newTestInjector(t, mock, true)
		req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform error: %v", err)
		}
		if !containsString(req.Metadata.Warnings, "injected_engrams:e1,e2,e3") {
			t.Errorf("warnings = %v, want injected_engrams:e1,e2,e3", req.Metadata.Warnings)
		}
	})
}

// TestInjectorMultiPassIdempotent verifies that repeated Transform passes
// keep exactly one marker Part (memory-single-block) and refresh its content.
func TestInjectorMultiPassIdempotent(t *testing.T) {
	mock := &mockMCPClient{CallToolFunc: recall("recall A")}
	inj, _ := newTestInjector(t, mock, true)
	req := &sessctx.ContextRequest{History: []*llm.Content{
		{Role: "system", Parts: []*llm.Part{{Text: "base"}, {Text: memoryHeader + "\n\nold recall\n" + memoryFooter}}},
		{Role: "user", Parts: []*llm.Part{{Text: "hi"}}},
	}}
	if err := inj.Transform(context.Background(), req); err != nil {
		t.Fatalf("first pass error: %v", err)
	}
	if countMarkerParts(req.History) != 1 {
		t.Fatalf("pass 1: expected exactly one marker part, got %d", countMarkerParts(req.History))
	}
	mock.CallToolFunc = recall("recall B")
	if err := inj.Transform(context.Background(), req); err != nil {
		t.Fatalf("second pass error: %v", err)
	}
	if countMarkerParts(req.History) != 1 {
		t.Errorf("pass 2: expected exactly one marker part, got %d", countMarkerParts(req.History))
	}
	foundB := false
	for _, p := range req.History[0].Parts {
		if strings.Contains(p.Text, "recall B") {
			foundB = true
		}
	}
	if !foundB {
		t.Error("second pass did not refresh the block content")
	}
}

// TestInjectorPriority verifies the mandated pipeline priority.
func TestInjectorPriority(t *testing.T) {
	inj, _ := newTestInjector(t, nil, true)
	if got := inj.Priority(); got != 15 {
		t.Errorf("Priority() = %d, want 15", got)
	}
}

// TestInjectorScope verifies the scope arg is forwarded when set and omitted
// when empty, plus task/budget forwarding.
func TestInjectorScope(t *testing.T) {
	t.Run("scope set", func(t *testing.T) {
		memCfg := config.DefaultMemoryConfig()
		memCfg.Enabled = true
		memCfg.Scope = "team-x"
		cfgPtr := &atomic.Pointer[config.MemoryConfig]{}
		cfgPtr.Store(&memCfg)
		var gotArgs map[string]interface{}
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotArgs = args
			return tools.ToolResult{Text: "recall"}, nil
		}}
		inj := NewPlurInjector(mock, cfgPtr, &ports.NoOpLogger{}).(*plurInjector)
		req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform error: %v", err)
		}
		if gotArgs["scope"] != "team-x" {
			t.Errorf("scope arg = %v, want team-x", gotArgs["scope"])
		}
		if gotArgs["task"] != "hi" {
			t.Errorf("task arg = %v, want hi", gotArgs["task"])
		}
		if gotArgs["budget"] != 2000 {
			t.Errorf("budget arg = %v, want 2000", gotArgs["budget"])
		}
	})

	t.Run("scope empty omitted", func(t *testing.T) {
		memCfg := config.DefaultMemoryConfig()
		memCfg.Enabled = true
		cfgPtr := &atomic.Pointer[config.MemoryConfig]{}
		cfgPtr.Store(&memCfg)
		var gotArgs map[string]interface{}
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotArgs = args
			return tools.ToolResult{Text: "recall"}, nil
		}}
		inj := NewPlurInjector(mock, cfgPtr, &ports.NoOpLogger{}).(*plurInjector)
		req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform error: %v", err)
		}
		if _, ok := gotArgs["scope"]; ok {
			t.Errorf("scope key must be omitted when empty, got %v", gotArgs["scope"])
		}
	})
}

// containsString reports whether s appears in the slice.
func containsString(haystack []string, s string) bool {
	for _, v := range haystack {
		if v == s {
			return true
		}
	}
	return false
}

// TestInjectorStripOnResultError verifies the Seam A strip on server-side
// isError rejections: the adapter returns ToolResult.Error with a nil Go
// error (issue #1410), and Transform must never build a recall block from
// it — nothing inserted when no block exists, and an existing marker block
// stripped.
func TestInjectorStripOnResultError(t *testing.T) {
	reject := func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "missing required parameter: summary", Error: errors.New("missing required parameter: summary")}, nil
	}

	t.Run("no prior block nothing inserted", func(t *testing.T) {
		mock := &mockMCPClient{CallToolFunc: reject}
		inj, _ := newTestInjector(t, mock, true)
		req := &sessctx.ContextRequest{History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform must return nil on result.Error, got %v", err)
		}
		if countMarkerParts(req.History) != 0 {
			t.Error("no memory block may be built from an error result")
		}
		if req.PersistHistory {
			t.Error("PersistHistory must not be set on the fail-open strip path")
		}
	})

	t.Run("existing block stripped", func(t *testing.T) {
		mock := &mockMCPClient{CallToolFunc: reject}
		inj, _ := newTestInjector(t, mock, true)
		req := &sessctx.ContextRequest{History: []*llm.Content{
			{Role: "system", Parts: []*llm.Part{{Text: "base"}, {Text: memoryHeader + "\n\nstale recall\n" + memoryFooter}}},
			{Role: "user", Parts: []*llm.Part{{Text: "hi"}}},
		}}
		if err := inj.Transform(context.Background(), req); err != nil {
			t.Fatalf("Transform must return nil on result.Error, got %v", err)
		}
		if countMarkerParts(req.History) != 0 {
			t.Error("marker block must be stripped on result.Error")
		}
		sys := req.History[0]
		if len(sys.Parts) != 1 || strings.Contains(sys.Parts[0].Text, memoryMarker) {
			t.Errorf("marker not stripped in memory: %+v", sys.Parts)
		}
	})
}
