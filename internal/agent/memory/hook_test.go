// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// turn builds a minimal Turn for hook tests.
func turn(index int, sessionID string, state *orchestrator.TurnState) *orchestrator.Turn {
	return &orchestrator.Turn{
		Index:     index,
		SessionID: sessionID,
		Mode:      "coder",
		State:     state,
	}
}

func responseTurn(index int, sessionID, text string) *orchestrator.Turn {
	return turn(index, sessionID, &orchestrator.TurnState{
		Response: &llm.Content{Role: "model", Parts: []*llm.Part{{Text: text}}},
	})
}

// TestHookTierOff verifies the off tier: no MCP calls, buffer untouched.
func TestHookTierOff(t *testing.T) {
	mock := &mockMCPClient{}
	h, _ := newTestHook(t, mock, config.MemoryLearnOff, &stubHistoryManager{})
	h.AfterTurn(responseTurn(0, "s1", "hello"), nil)

	if calls := mock.recordedNames(); len(calls) != 0 {
		t.Errorf("off tier must not call MCP, got %v", calls)
	}
	h.mu.Lock()
	bufCount := len(h.buffers)
	h.mu.Unlock()
	if bufCount != 0 {
		t.Errorf("off tier must not buffer, got %d session buffers", bufCount)
	}
}

// TestHookCaptureBranchI covers branch (i): Response present — with and
// without an error annotation. The plur_capture payload is exactly
// {summary, agent, session_id}; text/error/prompt keys are gone (issue
// #1410).
func TestHookCaptureBranchI(t *testing.T) {
	t.Run("response nil err", func(t *testing.T) {
		mock := &mockMCPClient{}
		var gotName string
		var gotArgs map[string]interface{}
		mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotName = name
			gotArgs = args
			return tools.ToolResult{}, nil
		}
		h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})
		h.AfterTurn(responseTurn(0, "s1", "hello"), nil)

		if gotName != "plur_capture" {
			t.Errorf("tool = %q, want plur_capture", gotName)
		}
		if len(gotArgs) != 3 {
			t.Errorf("args count = %d, want exactly 3, got %v", len(gotArgs), gotArgs)
		}
		if gotArgs["summary"] != "hello" {
			t.Errorf("summary = %v, want hello", gotArgs["summary"])
		}
		if gotArgs["agent"] != "coder" {
			t.Errorf("agent = %v, want coder", gotArgs["agent"])
		}
		if gotArgs["session_id"] != "s1" {
			t.Errorf("session_id = %v, want s1", gotArgs["session_id"])
		}
		for _, key := range []string{"text", "error", "prompt"} {
			if _, ok := gotArgs[key]; ok {
				t.Errorf("%s key must be absent, got %v", key, gotArgs)
			}
		}
	})

	t.Run("response with err annotated", func(t *testing.T) {
		mock := &mockMCPClient{}
		var gotArgs map[string]interface{}
		mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotArgs = args
			return tools.ToolResult{}, nil
		}
		h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})
		h.AfterTurn(responseTurn(0, "s1", "hello"), errors.New("boom"))

		// The text wins over the error annotation (branch (i) with err —
		// the T1-revised buildCaptureSummary discriminates on text presence).
		if gotArgs["summary"] != "hello" {
			t.Errorf("summary = %v, want hello (text wins)", gotArgs["summary"])
		}
		if gotArgs["agent"] != "coder" {
			t.Errorf("agent = %v, want coder", gotArgs["agent"])
		}
		if gotArgs["session_id"] != "s1" {
			t.Errorf("session_id = %v, want s1", gotArgs["session_id"])
		}
		for _, key := range []string{"text", "error", "prompt"} {
			if _, ok := gotArgs[key]; ok {
				t.Errorf("%s key must be absent, got %v", key, gotArgs)
			}
		}
	})
}

// TestHookCaptureBranchII covers branch (ii): Response nil + error. The
// error-first fold lands in the summary (buildCaptureSummary); the payload
// stays exactly {summary, agent, session_id} (issue #1410).
func TestHookCaptureBranchII(t *testing.T) {
	t.Run("non-transient error captures error + prompt", func(t *testing.T) {
		mock := &mockMCPClient{}
		var gotArgs map[string]interface{}
		mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotArgs = args
			return tools.ToolResult{}, nil
		}
		h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})
		tun := turn(1, "s1", &orchestrator.TurnState{
			PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "fix the bug"}}}},
		})
		h.AfterTurn(tun, errors.New("inference failed"))

		// The error-first fold: "error: <Error>", with " | user: <Prompt>"
		// folded in when it fits — verify the fit rule yields exactly this.
		if gotArgs["summary"] != "error: inference failed | user: fix the bug" {
			t.Errorf("summary = %v, want %q", gotArgs["summary"], "error: inference failed | user: fix the bug")
		}
		if len(gotArgs) != 3 {
			t.Errorf("args count = %d, want exactly 3, got %v", len(gotArgs), gotArgs)
		}
		if gotArgs["agent"] != "coder" {
			t.Errorf("agent = %v, want coder", gotArgs["agent"])
		}
		if gotArgs["session_id"] != "s1" {
			t.Errorf("session_id = %v, want s1", gotArgs["session_id"])
		}
		for _, key := range []string{"text", "error", "prompt"} {
			if _, ok := gotArgs[key]; ok {
				t.Errorf("%s key must be absent, got %v", key, gotArgs)
			}
		}
	})

	t.Run("transient error skipped with debug record", func(t *testing.T) {
		mock := &mockMCPClient{}
		h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})
		tun := turn(1, "s1", &orchestrator.TurnState{
			PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "fix"}}}},
		})
		// "max retries reached" wrap must unwrap via errors.Is.
		h.AfterTurn(tun, fmt.Errorf("max retries reached: %w", llm.ErrTransient))

		if calls := mock.recordedNames(); len(calls) != 0 {
			t.Errorf("transient error must produce no episode, got %v", calls)
		}
		h.mu.Lock()
		bufCount := len(h.buffers)
		h.mu.Unlock()
		if bufCount != 0 {
			t.Errorf("transient error must not buffer, got %d session buffers", bufCount)
		}
	})
}

// TestHookCaptureBranchIII covers branch (iii): Response nil + nil err —
// the episode is sourced from GetLastModelTurn; content without text parts
// produces no episode.
func TestHookCaptureBranchIII(t *testing.T) {
	t.Run("model turn with text", func(t *testing.T) {
		mock := &mockMCPClient{}
		var gotArgs map[string]interface{}
		mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotArgs = args
			return tools.ToolResult{}, nil
		}
		stub := &stubHistoryManager{
			GetLastModelTurnFunc: func(ctx context.Context) (int, *llm.Content, error) {
				return 3, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "model text"}}}, nil
			},
		}
		h, _ := newTestHook(t, mock, config.MemoryLearnCapture, stub)
		h.AfterTurn(turn(0, "s1", &orchestrator.TurnState{}), nil)

		if gotArgs["summary"] != "model text" {
			t.Errorf("summary = %v, want model text", gotArgs["summary"])
		}
	})

	t.Run("model turn without text parts filtered", func(t *testing.T) {
		mock := &mockMCPClient{}
		stub := &stubHistoryManager{
			GetLastModelTurnFunc: func(ctx context.Context) (int, *llm.Content, error) {
				return 3, &llm.Content{Role: "model", Parts: []*llm.Part{
					{Text: "", IsThought: true},
					{Text: ""},
				}}, nil
			},
		}
		h, _ := newTestHook(t, mock, config.MemoryLearnCapture, stub)
		h.AfterTurn(turn(0, "s1", &orchestrator.TurnState{}), nil)

		if calls := mock.recordedNames(); len(calls) != 0 {
			t.Errorf("text-parts filter must suppress tool-iteration turns, got %v", calls)
		}
	})

	t.Run("GetLastModelTurn failure logged and skipped", func(t *testing.T) {
		mock := &mockMCPClient{}
		stub := &stubHistoryManager{
			GetLastModelTurnFunc: func(ctx context.Context) (int, *llm.Content, error) {
				return 0, nil, errors.New("history missing")
			},
		}
		h, _ := newTestHook(t, mock, config.MemoryLearnCapture, stub)
		h.AfterTurn(turn(0, "s1", &orchestrator.TurnState{}), nil)

		if calls := mock.recordedNames(); len(calls) != 0 {
			t.Errorf("failed GetLastModelTurn must produce no episode, got %v", calls)
		}
	})
}

// TestHookBatch covers the batch tier: AfterTurn appends to the ring buffer
// (no MCP call); FlushSession drains under lock, deletes the map entry, and
// calls plur_learn_batch outside the lock with the {engrams: []engramPayload,
// max_llm_calls: 0} payload (no top-level session_id — issue #1410; the
// max_llm_calls: 0 bound pins issue #1412); a second flush is a no-op.
func TestHookBatch(t *testing.T) {
	mock := &mockMCPClient{}
	h, _ := newTestHook(t, mock, config.MemoryLearnBatch, &stubHistoryManager{})

	h.AfterTurn(responseTurn(0, "s1", "first"), nil)
	h.AfterTurn(responseTurn(1, "s1", "second"), nil)

	if calls := mock.recordedNames(); len(calls) != 0 {
		t.Fatalf("batch tier must not call MCP on AfterTurn, got %v", calls)
	}
	h.mu.Lock()
	buf, ok := h.buffers["s1"]
	h.mu.Unlock()
	if !ok {
		t.Fatal("buffer missing for session s1")
	}
	if len(buf.episodes) != 2 {
		t.Fatalf("buffer episodes = %d, want 2", len(buf.episodes))
	}

	var gotName string
	var gotArgs map[string]interface{}
	mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		gotName = name
		gotArgs = args
		return tools.ToolResult{}, nil
	}
	h.FlushSession("s1")

	if gotName != "plur_learn_batch" {
		t.Errorf("tool = %q, want plur_learn_batch", gotName)
	}
	if len(gotArgs) != 2 {
		t.Errorf("args count = %d, want exactly 2 (engrams + max_llm_calls), got %v", len(gotArgs), gotArgs)
	}
	if v, ok := gotArgs["max_llm_calls"]; !ok || v != 0 {
		t.Errorf("max_llm_calls = %v (present=%v), want 0", v, ok)
	}
	if _, ok := gotArgs["session_id"]; ok {
		t.Errorf("top-level session_id must be absent, got %v", gotArgs)
	}
	engrams, ok := gotArgs["engrams"].([]engramPayload)
	if !ok {
		t.Fatalf("engrams payload = %T, want []engramPayload", gotArgs["engrams"])
	}
	if len(engrams) != 2 {
		t.Fatalf("engrams length = %d, want 2", len(engrams))
	}
	for i, want := range []string{"first", "second"} {
		if engrams[i].Statement != want {
			t.Errorf("engrams[%d].Statement = %q, want %q", i, engrams[i].Statement, want)
		}
	}
	for i := range engrams {
		wantTags := []string{"session:s1", "mode:coder"}
		if !reflect.DeepEqual(engrams[i].Tags, wantTags) {
			t.Errorf("engrams[%d].Tags = %v, want %v", i, engrams[i].Tags, wantTags)
		}
		if engrams[i].Scope != "" {
			t.Errorf("engrams[%d].Scope = %q, want %q (unset default)", i, engrams[i].Scope, "")
		}
	}

	h.mu.Lock()
	_, stillPresent := h.buffers["s1"]
	h.mu.Unlock()
	if stillPresent {
		t.Error("buffer map entry must be deleted after flush")
	}

	// Second flush on the same session → no call (empty).
	before := len(mock.recordedNames())
	h.FlushSession("s1")
	if got := len(mock.recordedNames()); got != before {
		t.Errorf("second flush must not call MCP: %d -> %d calls", before, got)
	}

	t.Run("flush applies configured scope to every engram", func(t *testing.T) {
		mock := &mockMCPClient{}
		h, cfgPtr := newTestHook(t, mock, config.MemoryLearnBatch, &stubHistoryManager{})
		h.AfterTurn(responseTurn(0, "s1", "first"), nil)
		h.AfterTurn(responseTurn(1, "s1", "second"), nil)

		memCfg := cfgPtr.Load()
		memCfg.Scope = "team-x"
		cfgPtr.Store(memCfg)

		var gotArgs map[string]interface{}
		mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotArgs = args
			return tools.ToolResult{}, nil
		}
		h.FlushSession("s1")

		engrams, ok := gotArgs["engrams"].([]engramPayload)
		if !ok {
			t.Fatalf("engrams payload = %T, want []engramPayload", gotArgs["engrams"])
		}
		if len(engrams) != 2 {
			t.Fatalf("engrams length = %d, want 2", len(engrams))
		}
		for i := range engrams {
			if engrams[i].Scope != "team-x" {
				t.Errorf("engrams[%d].Scope = %q, want %q", i, engrams[i].Scope, "team-x")
			}
		}
	})
}

// TestHookFull covers the full tier: gated direct learn on correction frames,
// sha256 dedupe, flood bound, and non-frame suppression — with the ring
// buffer appended on every turn.
func TestHookFull(t *testing.T) {
	t.Run("frame match learns", func(t *testing.T) {
		var learnArgs []map[string]interface{}
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			if name == "plur_learn" {
				learnArgs = append(learnArgs, args)
			}
			return tools.ToolResult{}, nil
		}}
		h, cfgPtr := newTestHook(t, mock, config.MemoryLearnFull, &stubHistoryManager{})
		memCfg := cfgPtr.Load()
		memCfg.Scope = "team-x"
		cfgPtr.Store(memCfg)
		tun := turn(0, "s1", &orchestrator.TurnState{
			Response:        &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
			PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "please remember to always use X"}}}},
		})
		h.AfterTurn(tun, nil)

		if len(learnArgs) != 1 {
			t.Fatalf("learn calls = %d, want 1", len(learnArgs))
		}
		if learnArgs[0]["statement"] != "please remember to always use X" {
			t.Errorf("statement = %v", learnArgs[0]["statement"])
		}
		wantTags := []string{"session:s1", "mode:coder"}
		if !reflect.DeepEqual(learnArgs[0]["tags"], wantTags) {
			t.Errorf("tags = %v, want %v", learnArgs[0]["tags"], wantTags)
		}
		if learnArgs[0]["scope"] != "team-x" {
			t.Errorf("scope = %v, want team-x", learnArgs[0]["scope"])
		}
		for _, key := range []string{"agent", "session_id"} {
			if _, ok := learnArgs[0][key]; ok {
				t.Errorf("%s key must be absent on plur_learn, got %v", key, learnArgs[0])
			}
		}
	})

	t.Run("learn omits scope when unset", func(t *testing.T) {
		var learnArgs []map[string]interface{}
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			if name == "plur_learn" {
				learnArgs = append(learnArgs, args)
			}
			return tools.ToolResult{}, nil
		}}
		h, _ := newTestHook(t, mock, config.MemoryLearnFull, &stubHistoryManager{}) // default Scope == ""
		tun := turn(0, "s1", &orchestrator.TurnState{
			Response:        &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
			PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "please remember to always use Y"}}}},
		})
		h.AfterTurn(tun, nil)

		if len(learnArgs) != 1 {
			t.Fatalf("learn calls = %d, want 1", len(learnArgs))
		}
		if _, ok := learnArgs[0]["scope"]; ok {
			t.Errorf("scope key must be absent when Scope == \"\", got %v", learnArgs[0])
		}
	})

	t.Run("hash dedupe identical statement", func(t *testing.T) {
		learnCount := 0
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			if name == "plur_learn" {
				learnCount++
			}
			return tools.ToolResult{}, nil
		}}
		h, _ := newTestHook(t, mock, config.MemoryLearnFull, &stubHistoryManager{})
		for i := 0; i < 2; i++ {
			tun := turn(i, "s1", &orchestrator.TurnState{
				Response:        &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
				PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "please remember to use X"}}}},
			})
			h.AfterTurn(tun, nil)
		}
		if learnCount != 1 {
			t.Errorf("learn calls = %d, want 1 (dedupe)", learnCount)
		}
	})

	t.Run("flood bound", func(t *testing.T) {
		learnCount := 0
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			if name == "plur_learn" {
				learnCount++
			}
			return tools.ToolResult{}, nil
		}}
		h, _ := newTestHook(t, mock, config.MemoryLearnFull, &stubHistoryManager{})
		statements := []string{
			"please remember to use X1",
			"please remember to use X2",
			"please remember to use X3",
			"please remember to use X4", // 4th distinct → flood bound
		}
		for i, s := range statements {
			tun := turn(i, "s1", &orchestrator.TurnState{
				Response:        &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
				PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: s}}}},
			})
			h.AfterTurn(tun, nil)
		}
		if learnCount != 3 {
			t.Errorf("learn calls = %d, want 3 (flood bound)", learnCount)
		}
	})

	t.Run("non-frame no learn but buffer appended", func(t *testing.T) {
		learnCount := 0
		mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			if name == "plur_learn" {
				learnCount++
			}
			return tools.ToolResult{}, nil
		}}
		h, _ := newTestHook(t, mock, config.MemoryLearnFull, &stubHistoryManager{})
		tun := turn(0, "s1", &orchestrator.TurnState{
			Response:        &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
			PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "what time is it"}}}},
		})
		h.AfterTurn(tun, nil)

		if learnCount != 0 {
			t.Errorf("learn calls = %d, want 0 for non-frame", learnCount)
		}
		h.mu.Lock()
		buf, ok := h.buffers["s1"]
		h.mu.Unlock()
		if !ok || len(buf.episodes) != 1 {
			t.Errorf("full tier must still append to the ring buffer, got ok=%v", ok)
		}
	})
}

// TestHookTurnDedupe verifies the turn-scoped dedupe: the same
// SessionID+Index twice → the second AfterTurn is a no-op.
func TestHookTurnDedupe(t *testing.T) {
	mock := &mockMCPClient{}
	h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})
	tun := responseTurn(0, "s1", "hello")
	h.AfterTurn(tun, nil)
	h.AfterTurn(tun, nil)
	if calls := mock.recordedNames(); len(calls) != 1 {
		t.Errorf("expected exactly 1 capture after dedupe, got %v", calls)
	}
}

// TestHookNilClient verifies the nil-client runtime guard: no panic, no MCP
// call.
func TestHookNilClient(t *testing.T) {
	h, _ := newTestHook(t, nil, config.MemoryLearnCapture, &stubHistoryManager{})
	h.AfterTurn(responseTurn(0, "s1", "hello"), nil) // must not panic
}

func TestHookNilClientFlush(t *testing.T) {
	h, _ := newTestHook(t, nil, config.MemoryLearnBatch, &stubHistoryManager{})
	h.AfterTurn(responseTurn(0, "s1", "hello"), nil)
	h.FlushSession("s1") // must not panic
}

// TestHookNilCfg verifies the nil-config guard: AfterTurn returns immediately.
func TestHookNilCfg(t *testing.T) {
	mock := &mockMCPClient{}
	cfgPtr := &atomic.Pointer[config.MemoryConfig]{}
	cfgPtr.Store(nil)
	h := &plurHook{client: mock, cfg: cfgPtr, logger: nil}
	h.AfterTurn(responseTurn(0, "s1", "hello"), nil) // must not panic
	if calls := mock.recordedNames(); len(calls) != 0 {
		t.Errorf("nil cfg must be a no-op, got %v", calls)
	}
}

// TestHookConcurrentAfterTurnAndFlush exercises concurrent AfterTurn (batch
// tier) + FlushSession under the race detector: buffer access is mutex-
// guarded and the MCP call never holds the lock.
func TestHookConcurrentAfterTurnAndFlush(t *testing.T) {
	mock := &mockMCPClient{}
	h, _ := newTestHook(t, mock, config.MemoryLearnBatch, &stubHistoryManager{})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h.AfterTurn(responseTurn(i, "s1", fmt.Sprintf("resp %d", i)), nil)
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.FlushSession("s1")
	}()
	wg.Wait()

	// No panic and race-clean are the assertions; the buffer may hold
	// episodes appended after the drain, which is valid behavior.
	h.mu.Lock()
	_ = h.buffers
	h.mu.Unlock()
}

// TestHookWriteSitesTreatResultErrorAsFailure pins the domain-outcome
// detection at all three write sites: the MCP adapter surfaces server-side
// isError rejections as ToolResult.Error with a NIL Go error (issue #1410),
// so each site must reach the detection check (no panic, call recorded)
// without discarding the result before it. Counting assertions land in T3.
func TestHookWriteSitesTreatResultErrorAsFailure(t *testing.T) {
	reject := func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
	}

	tests := []struct {
		name  string
		tier  config.MemoryLearnTier
		tool  string
		drive func(h *plurHook)
	}{
		{
			name: "plur_capture",
			tier: config.MemoryLearnCapture,
			tool: "plur_capture",
			drive: func(h *plurHook) {
				h.AfterTurn(responseTurn(0, "s1", "hello"), nil)
			},
		},
		{
			name: "plur_learn",
			tier: config.MemoryLearnFull,
			tool: "plur_learn",
			drive: func(h *plurHook) {
				tun := turn(0, "s1", &orchestrator.TurnState{
					Response:        &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
					PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "please remember to use X"}}}},
				})
				h.AfterTurn(tun, nil)
			},
		},
		{
			name: "plur_learn_batch",
			tier: config.MemoryLearnBatch,
			tool: "plur_learn_batch",
			drive: func(h *plurHook) {
				h.AfterTurn(responseTurn(0, "s1", "first"), nil)
				h.AfterTurn(responseTurn(1, "s1", "second"), nil)
				h.FlushSession("s1")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMCPClient{CallToolFunc: reject}
			h, _ := newTestHook(t, mock, tt.tier, &stubHistoryManager{})
			tt.drive(h) // must not panic on result.Error != nil with nil Go err

			if calls := mock.recordedNames(); !containsString(calls, "CallTool:"+tt.tool) {
				t.Errorf("expected call to %s, got %v", tt.tool, calls)
			}
		})
	}
}

// TestHookWriteStats_AllFailedToolDead pins the per-tool all-or-nothing dead
// trigger on the simplest case: capture tier, 3 turns, every write rejected
// (the real isError convention: ToolResult.Error set, nil Go error). The
// report must surface the aggregate AND name plur_capture as dead.
func TestHookWriteStats_AllFailedToolDead(t *testing.T) {
	reject := func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
	}
	mock := &mockMCPClient{CallToolFunc: reject}
	h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})

	for i := 0; i < 3; i++ {
		h.AfterTurn(responseTurn(i, "s1", fmt.Sprintf("turn %d", i)), nil)
	}

	report := h.MemoryWriteReport("s1")
	if !strings.Contains(report, "memory write failures: 3") {
		t.Errorf("report = %q, want it to contain %q", report, "memory write failures: 3")
	}
	if !strings.Contains(report, "plur_capture failing — learning is disabled") {
		t.Errorf("report = %q, want it to contain %q", report, "plur_capture failing — learning is disabled")
	}
}

// TestHookWriteStats_TransientTolerance pins the non-dead boundary: 10
// attempts with exactly 9 failures (1 success). The aggregate still surfaces,
// but no tool is named dead — per-tool all-or-nothing tolerates transients.
func TestHookWriteStats_TransientTolerance(t *testing.T) {
	var calls int
	mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		calls++
		if calls <= 9 {
			return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
		}
		return tools.ToolResult{}, nil
	}}
	h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})

	for i := 0; i < 10; i++ {
		h.AfterTurn(responseTurn(i, "s1", fmt.Sprintf("turn %d", i)), nil)
	}

	report := h.MemoryWriteReport("s1")
	if !strings.Contains(report, "memory write failures: 9") {
		t.Errorf("report = %q, want it to contain %q", report, "memory write failures: 9")
	}
	if strings.Contains(report, "failing — learning is disabled") {
		t.Errorf("report = %q, must NOT name a dead tool (1 success among 10 attempts)", report)
	}
}

// TestHookWriteStats_PartialDrift pins the exact shipped scenario: the full
// tier is the only one that calls two write tools in one session. plur_learn
// succeeds (frame-match turn) while plur_learn_batch is rejected via
// FlushSession — partial schema drift on one tool must dead-name ONLY the
// failing tool.
func TestHookWriteStats_PartialDrift(t *testing.T) {
	mock := &mockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		if name == "plur_learn_batch" {
			return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
		}
		return tools.ToolResult{}, nil
	}}
	h, _ := newTestHook(t, mock, config.MemoryLearnFull, &stubHistoryManager{})
	tun := turn(0, "s1", &orchestrator.TurnState{
		Response:        &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
		PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "please remember to use X"}}}},
	})
	h.AfterTurn(tun, nil)
	h.FlushSession("s1")

	report := h.MemoryWriteReport("s1")
	if !strings.Contains(report, "plur_learn_batch failing — learning is disabled") {
		t.Errorf("report = %q, want it to name plur_learn_batch as dead", report)
	}
	if strings.Contains(report, "plur_learn failing") {
		t.Errorf("report = %q, must NOT name plur_learn (it succeeded)", report)
	}
}

// TestHookWriteStats_FlushAttemptIncluded proves the flush's own attempt is
// counted BEFORE the report is read — the T4 flush-then-read ordering in
// Chat's defer depends on this. Batch tier: 1 buffered turn, FlushSession
// rejected → 1 failure on plur_learn_batch, named dead.
func TestHookWriteStats_FlushAttemptIncluded(t *testing.T) {
	mock := &mockMCPClient{}
	h, _ := newTestHook(t, mock, config.MemoryLearnBatch, &stubHistoryManager{})
	h.AfterTurn(responseTurn(0, "s1", "first"), nil)

	mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
	}
	h.FlushSession("s1")

	report := h.MemoryWriteReport("s1")
	if !strings.Contains(report, "memory write failures: 1") {
		t.Errorf("report = %q, want it to contain %q", report, "memory write failures: 1")
	}
	if !strings.Contains(report, "plur_learn_batch failing — learning is disabled") {
		t.Errorf("report = %q, want it to name plur_learn_batch as dead", report)
	}
}

// TestHookWriteStats_NoFailuresEmpty pins the empty-report contract: a
// fully-successful session yields "" (the accessor reads no failure signal,
// so Chat's defer has nothing to surface).
func TestHookWriteStats_NoFailuresEmpty(t *testing.T) {
	mock := &mockMCPClient{}
	h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})

	for i := 0; i < 3; i++ {
		h.AfterTurn(responseTurn(i, "s1", fmt.Sprintf("turn %d", i)), nil)
	}

	if report := h.MemoryWriteReport("s1"); report != "" {
		t.Errorf("report = %q, want empty string (zero failures)", report)
	}
}

// TestHookWriteStats_ReportDeletesEntry pins delete-on-read: after a failed
// session's report, a second read of the same session returns "" — bounded
// map growth, the next session starts fresh.
func TestHookWriteStats_ReportDeletesEntry(t *testing.T) {
	reject := func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
	}
	mock := &mockMCPClient{CallToolFunc: reject}
	h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})
	h.AfterTurn(responseTurn(0, "s1", "hello"), nil)

	if first := h.MemoryWriteReport("s1"); first == "" {
		t.Fatal("first report must be non-empty (1 failure recorded)")
	}
	if second := h.MemoryWriteReport("s1"); second != "" {
		t.Errorf("second report = %q, want empty string (delete-on-read)", second)
	}
}

// recordingLogger is a concurrency-safe ports.Logger double (ADR-021) that
// records every Warn call as a (msg, kv) pair for assertion. Error/Info/
// Debug are no-ops — the hook's failure surface is Warn-only.
type recordingLogger struct {
	mu    sync.Mutex
	warns []warnRecord
}

// warnRecord is one recorded Warn call: the message and its key/value pairs.
type warnRecord struct {
	msg string
	kv  map[string]interface{}
}

// Warn implements ports.Logger.
func (l *recordingLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kv := make(map[string]interface{}, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		kv[fmt.Sprintf("%v", args[i])] = args[i+1]
	}
	l.warns = append(l.warns, warnRecord{msg: msg, kv: kv})
}

// Error implements ports.Logger.
func (l *recordingLogger) Error(msg string, args ...any) {}

// Info implements ports.Logger.
func (l *recordingLogger) Info(msg string, args ...any) {}

// Debug implements ports.Logger.
func (l *recordingLogger) Debug(msg string, args ...any) {}

// warnValue returns the value for key in the first recorded Warn with msg,
// or (nil, false) when no such Warn or key exists.
func (l *recordingLogger) warnValue(msg, key string) (interface{}, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, w := range l.warns {
		if w.msg != msg {
			continue
		}
		v, ok := w.kv[key]
		return v, ok
	}
	return nil, false
}

// newTestHookWithLogger builds a plurHook like newTestHook but with the
// given logger (e.g. a *recordingLogger for Warn assertions). HOME is
// redirected to a temp dir so flock side effects never touch the real
// ~/.plur.
func newTestHookWithLogger(t *testing.T, client tools.MCPClient, tier config.MemoryLearnTier, hist ports.HistoryManager, logger ports.Logger) (*plurHook, *atomic.Pointer[config.MemoryConfig]) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	memCfg := config.DefaultMemoryConfig()
	memCfg.Enabled = true
	memCfg.LearnTier = tier
	cfgPtr := &atomic.Pointer[config.MemoryConfig]{}
	cfgPtr.Store(&memCfg)
	return NewPlurHook(client, cfgPtr, logger, &clock.FakeClock{}, hist).(*plurHook), cfgPtr
}

// episodeTexts returns the Text of each episode in order — the assertion
// projection for buffer-order checks.
func episodeTexts(eps []episode) []string {
	texts := make([]string, len(eps))
	for i, e := range eps {
		texts[i] = e.Text
	}
	return texts
}

// TestHookBatchRetainOnFailure pins the issue #1412 retain-on-failure
// contract: a failed batch write restores the claimed episodes at the front
// of the buffer (newer appends stay after them) and records the failure
// Warn; the next flush writes all episodes exactly once with
// max_llm_calls: 0 and removes the (now-empty) buffer entry.
func TestHookBatchRetainOnFailure(t *testing.T) {
	logger := &recordingLogger{}
	mock := &mockMCPClient{}
	h, _ := newTestHookWithLogger(t, mock, config.MemoryLearnBatch, &stubHistoryManager{}, logger)

	h.AfterTurn(responseTurn(0, "s1", "first"), nil)
	h.AfterTurn(responseTurn(1, "s1", "second"), nil)

	// First flush: rejected (the real isError convention — ToolResult.Error
	// set, nil Go error).
	mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
	}
	h.FlushSession("s1")

	// Claimed episodes restored at the front — the buffer must survive with
	// both episodes, in order, for the next flush opportunity.
	h.mu.Lock()
	buf, ok := h.buffers["s1"]
	h.mu.Unlock()
	if !ok || !reflect.DeepEqual(episodeTexts(buf.episodes), []string{"first", "second"}) {
		t.Fatalf("buffer after failed flush = %v (present=%v), want [first second]", episodeTexts(buf.episodes), ok)
	}
	if _, ok := logger.warnValue("memory_learn_batch_failed", "retained"); !ok {
		t.Error("failed flush must record the memory_learn_batch_failed Warn")
	}

	// Second flush: succeeds and must write both retained episodes exactly
	// once, still with max_llm_calls: 0.
	var gotArgs map[string]interface{}
	mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		gotArgs = args
		return tools.ToolResult{}, nil
	}
	h.FlushSession("s1")

	engrams, ok := gotArgs["engrams"].([]engramPayload)
	if !ok {
		t.Fatalf("engrams payload = %T, want []engramPayload", gotArgs["engrams"])
	}
	if len(engrams) != 2 || !reflect.DeepEqual([]string{engrams[0].Statement, engrams[1].Statement}, []string{"first", "second"}) {
		t.Fatalf("engrams = %+v, want statements [first second]", engrams)
	}
	if v, ok := gotArgs["max_llm_calls"]; !ok || v != 0 {
		t.Errorf("max_llm_calls = %v (present=%v), want 0", v, ok)
	}
	h.mu.Lock()
	_, stillPresent := h.buffers["s1"]
	h.mu.Unlock()
	if stillPresent {
		t.Error("buffer map entry must be deleted after successful flush")
	}
}

// TestHookBatchDropCountReported pins the issue #1412 ring-bound drop
// report: after maxBufferEpisodes+5 appends, 5 oldest episodes were evicted
// at the ring bound; the failed flush must restore the 20 retained episodes
// and surface dropped=5 with retained=20 on the memory_learn_batch_failed
// Warn.
func TestHookBatchDropCountReported(t *testing.T) {
	logger := &recordingLogger{}
	mock := &mockMCPClient{}
	h, _ := newTestHookWithLogger(t, mock, config.MemoryLearnBatch, &stubHistoryManager{}, logger)

	mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "rejected", Error: errors.New("rejected")}, nil
	}
	for i := 0; i < maxBufferEpisodes+5; i++ {
		h.AfterTurn(responseTurn(i, "s1", fmt.Sprintf("turn-%d", i)), nil)
	}
	h.FlushSession("s1")

	if v, ok := logger.warnValue("memory_learn_batch_failed", "dropped"); !ok || v != 5 {
		t.Errorf("memory_learn_batch_failed dropped = %v (present=%v), want 5", v, ok)
	}
	if v, ok := logger.warnValue("memory_learn_batch_failed", "retained"); !ok || v != 20 {
		t.Errorf("memory_learn_batch_failed retained = %v (present=%v), want 20", v, ok)
	}
	h.mu.Lock()
	buf, ok := h.buffers["s1"]
	h.mu.Unlock()
	if !ok || len(buf.episodes) != maxBufferEpisodes {
		t.Errorf("buffer episodes after failed flush = %d (present=%v), want %d", len(buf.episodes), ok, maxBufferEpisodes)
	}
}
