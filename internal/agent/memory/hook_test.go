// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
	mock := &MockMCPClient{}
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
// without an error annotation.
func TestHookCaptureBranchI(t *testing.T) {
	t.Run("response nil err", func(t *testing.T) {
		mock := &MockMCPClient{}
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
		if gotArgs["agent"] != "coder" || gotArgs["session_id"] != "s1" || gotArgs["text"] != "hello" {
			t.Errorf("args = %v, want agent/session_id/text", gotArgs)
		}
		if _, ok := gotArgs["error"]; ok {
			t.Errorf("error key must be omitted when err is nil, got %v", gotArgs)
		}
		if _, ok := gotArgs["prompt"]; ok {
			t.Errorf("prompt key must be omitted on branch (i), got %v", gotArgs)
		}
	})

	t.Run("response with err annotated", func(t *testing.T) {
		mock := &MockMCPClient{}
		var gotArgs map[string]interface{}
		mock.CallToolFunc = func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotArgs = args
			return tools.ToolResult{}, nil
		}
		h, _ := newTestHook(t, mock, config.MemoryLearnCapture, &stubHistoryManager{})
		h.AfterTurn(responseTurn(0, "s1", "hello"), errors.New("boom"))

		if gotArgs["text"] != "hello" {
			t.Errorf("text = %v, want hello", gotArgs["text"])
		}
		if gotArgs["error"] != "boom" {
			t.Errorf("error = %v, want boom", gotArgs["error"])
		}
	})
}

// TestHookCaptureBranchII covers branch (ii): Response nil + error.
func TestHookCaptureBranchII(t *testing.T) {
	t.Run("non-transient error captures error + prompt", func(t *testing.T) {
		mock := &MockMCPClient{}
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

		if gotArgs["error"] != "inference failed" {
			t.Errorf("error = %v, want inference failed", gotArgs["error"])
		}
		if gotArgs["prompt"] != "fix the bug" {
			t.Errorf("prompt = %v, want fix the bug", gotArgs["prompt"])
		}
		if _, ok := gotArgs["text"]; ok {
			t.Errorf("text key must be omitted on branch (ii), got %v", gotArgs)
		}
	})

	t.Run("transient error skipped with debug record", func(t *testing.T) {
		mock := &MockMCPClient{}
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
		mock := &MockMCPClient{}
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

		if gotArgs["text"] != "model text" {
			t.Errorf("text = %v, want model text", gotArgs["text"])
		}
	})

	t.Run("model turn without text parts filtered", func(t *testing.T) {
		mock := &MockMCPClient{}
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
		mock := &MockMCPClient{}
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
// calls plur_learn_batch outside the lock; a second flush is a no-op.
func TestHookBatch(t *testing.T) {
	mock := &MockMCPClient{}
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
	if gotArgs["session_id"] != "s1" {
		t.Errorf("session_id = %v, want s1", gotArgs["session_id"])
	}
	eps, ok := gotArgs["episodes"].([]episode)
	if !ok {
		t.Fatalf("episodes payload = %T, want []episode", gotArgs["episodes"])
	}
	if len(eps) != 2 || eps[0].Text != "first" || eps[1].Text != "second" {
		t.Errorf("episodes payload wrong: %+v", eps)
	}
	if eps[0].Mode != "coder" || eps[0].SessionID != "s1" {
		t.Errorf("episode fields wrong: %+v", eps[0])
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
}

// TestHookFull covers the full tier: gated direct learn on correction frames,
// sha256 dedupe, flood bound, and non-frame suppression — with the ring
// buffer appended on every turn.
func TestHookFull(t *testing.T) {
	t.Run("frame match learns", func(t *testing.T) {
		var learnArgs []map[string]interface{}
		mock := &MockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			if name == "plur_learn" {
				learnArgs = append(learnArgs, args)
			}
			return tools.ToolResult{}, nil
		}}
		h, _ := newTestHook(t, mock, config.MemoryLearnFull, &stubHistoryManager{})
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
		if learnArgs[0]["agent"] != "coder" {
			t.Errorf("agent = %v, want coder", learnArgs[0]["agent"])
		}
	})

	t.Run("hash dedupe identical statement", func(t *testing.T) {
		learnCount := 0
		mock := &MockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
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
		mock := &MockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
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
		mock := &MockMCPClient{CallToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
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
	mock := &MockMCPClient{}
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
	mock := &MockMCPClient{}
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
	mock := &MockMCPClient{}
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
