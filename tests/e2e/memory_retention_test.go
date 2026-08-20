// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/memory"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/mcp"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// TestE2EMemoryBatchRetainedAcrossFailedFlush_RealStdioClient proves issue
// #1412's retain-on-failure contract at the wire level (AC3: "retained
// episodes are not lost") through the REAL StdioClient. The real plurHook
// (memory.NewPlurHook) is driven directly against the fake PLUR MCP server
// with a 4s FAKE_PLUR_DELAY_MS_FILE delay scoped to plur_learn_batch: flush
// 1 exceeds the hook's 3s detached context (detachedTimeout, ADR-068 §2.2)
// and fails with context deadline exceeded; the claimed episodes are
// restored (retained) for the next flush opportunity. The delay is then
// disarmed and a third episode appended; flush 2 succeeds and the fake's
// store holds "first", "second" AND "third" — if the failed flush had
// discarded the buffer, only "third" would exist.
//
// This leg is the sibling of
// TestE2EMemoryBatchSlowFake_SymptomReproduced (the symptom leg: the same
// failure through the real CLI, pinning the shipped log lines and the
// zero-persistence contract). This leg drives the hook directly — the
// Chat-defer flush path is already covered by the agent wiring test and the
// symptom CLI leg. The fake's delay file is re-read per call specifically so
// the delay can be disarmed between flushes without restarting the child
// (tests/e2e/testdata/fakeplur/main.go delayFor).
func TestE2EMemoryBatchRetainedAcrossFailedFlush_RealStdioClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	fakePlur := buildFakePlur(t)
	storePath := filepath.Join(t.TempDir(), "plur-store.json")
	delayPath := filepath.Join(t.TempDir(), "plur-delay-ms")
	if err := os.WriteFile(delayPath, []byte("4000"), 0644); err != nil {
		t.Fatalf("write delay file: %v", err)
	}
	// The hook's in-process advisory flock reads the TEST process HOME — keep
	// it out of the real home (mirrors newTestHook's t.Setenv("HOME", t.TempDir())).
	t.Setenv("HOME", t.TempDir())

	cfg := config.MCPServerConfig{
		Command: fakePlur,
		Env: map[string]string{
			"FAKE_PLUR_STORE":         storePath,
			"FAKE_PLUR_DELAY_MS_FILE": delayPath,
			"FAKE_PLUR_DELAY_TOOL":    "plur_learn_batch",
		},
	}
	client, err := mcp.NewStdioClient(cfg, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("failed to start fake PLUR StdioClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	memCfg := config.DefaultMemoryConfig()
	memCfg.Enabled = true
	memCfg.LearnTier = config.MemoryLearnBatch
	cfgPtr := &atomic.Pointer[config.MemoryConfig]{}
	cfgPtr.Store(&memCfg)
	logger := &capturingLogger{}
	hist := &stubHistoryManager{}
	hook := memory.NewPlurHook(client, cfgPtr, logger, clock.RealClock{}, hist)
	fs, ok := hook.(interface{ FlushSession(string) })
	if !ok {
		t.Fatal("plurHook must expose FlushSession for structural assertion")
	}

	// Flush 1: the fake's 4s delay exceeds the hook's 3s detached ctx → failure.
	hook.AfterTurn(responseTurn(0, "s1", "first"), nil)
	hook.AfterTurn(responseTurn(1, "s1", "second"), nil)
	fs.FlushSession("s1")

	// Shipped failure line through the real StdioClient.
	errVal, ok := logger.warnValue("memory_learn_batch_failed", "error")
	if !ok || !strings.Contains(fmt.Sprint(errVal), "context deadline exceeded") {
		t.Fatalf("expected memory_learn_batch_failed with 'context deadline exceeded', got err=%v (present=%v)", errVal, ok)
	}

	// Nothing persisted by the failed call (T1 contract: delayed call returns
	// isError without processing).
	st := readPlurStore(t, storePath)
	if len(st.Engrams) != 0 {
		t.Fatalf("failed flush must not persist; got %d engrams", len(st.Engrams))
	}

	// Disarm the delay, append a third episode, retry.
	if err := os.Remove(delayPath); err != nil {
		t.Fatalf("remove delay file: %v", err)
	}
	hook.AfterTurn(responseTurn(2, "s1", "third"), nil)
	fs.FlushSession("s1")

	// Retention proof: the retry wrote the two retained episodes PLUS the new
	// one — if the failed flush had discarded the buffer, only "third" would
	// exist.
	st = readPlurStore(t, storePath)
	assertEngramsPersisted(t, st, "first", "second", "third")
}

// assertEngramsPersisted fails the test when any of the given texts is
// missing from the fake's store — the retention proof projection of the
// retry leg (kept out of the test function to hold its cyclomatic complexity
// at the Architect's ≤ 10 bound).
func assertEngramsPersisted(t *testing.T, st plurStoreFile, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if _, ok := findEngramByText(st, want); !ok {
			t.Errorf("retained episode %q missing from store after retry; engrams: %+v", want, st.Engrams)
		}
	}
}

// Compile-time interface assertions for the hand-rolled doubles.
var (
	_ ports.Logger         = (*capturingLogger)(nil)
	_ ports.HistoryManager = (*stubHistoryManager)(nil)
)

// capturingLogger is a concurrency-safe ports.Logger double (ADR-021) that
// records every Warn call as a (msg, kv) pair for assertion. Error/Info/
// Debug are no-ops — the hook's failure surface is Warn-only.
type capturingLogger struct {
	mu    sync.Mutex
	warns []capturedWarn
}

// capturedWarn is one recorded Warn call: the message and its key/value pairs.
type capturedWarn struct {
	msg string
	kv  map[string]interface{}
}

// Warn implements ports.Logger.
func (l *capturingLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kv := make(map[string]interface{}, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		kv[fmt.Sprintf("%v", args[i])] = args[i+1]
	}
	l.warns = append(l.warns, capturedWarn{msg: msg, kv: kv})
}

// Error implements ports.Logger.
func (l *capturingLogger) Error(msg string, args ...any) {}

// Info implements ports.Logger.
func (l *capturingLogger) Info(msg string, args ...any) {}

// Debug implements ports.Logger.
func (l *capturingLogger) Debug(msg string, args ...any) {}

// warnValue returns the value for key in the first recorded Warn with msg,
// or (nil, false) when no such Warn or key exists.
func (l *capturingLogger) warnValue(msg, key string) (interface{}, bool) {
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

// stubHistoryManager implements ports.HistoryManager (16 methods — hand-rolled
// per the interface-satisfying-stub acceptance class, copied from
// internal/agent/memory/memory_test.go). Only GetLastModelTurn would ever be
// reachable, and branch (i) of AfterTurn never calls it; the rest return zero
// values.
type stubHistoryManager struct{}

func (s *stubHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	return nil, nil
}

func (s *stubHistoryManager) GetTotalEntries() int { return 0 }

func (s *stubHistoryManager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
}

func (s *stubHistoryManager) GetResolver() llm.AssetResolver { return nil }

func (s *stubHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	return nil
}

func (s *stubHistoryManager) AddContent(ctx context.Context, content *llm.Content) error {
	return nil
}

func (s *stubHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	return nil
}

func (s *stubHistoryManager) Save(ctx context.Context) error { return nil }

func (s *stubHistoryManager) Sync(ctx context.Context) error { return nil }

func (s *stubHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}

func (s *stubHistoryManager) SetPinned(ctx context.Context, turnID string, pinned bool) error {
	return nil
}

func (s *stubHistoryManager) GetFilePath() string { return "" }

func (s *stubHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	return 0, 0, 0, nil
}

func (s *stubHistoryManager) GetLastModelTurn(ctx context.Context) (int, *llm.Content, error) {
	return 0, nil, nil
}

func (s *stubHistoryManager) GetModelTurn(ctx context.Context, index int) (*llm.Content, error) {
	return nil, nil
}

func (s *stubHistoryManager) UpdateTurnContent(ctx context.Context, index int, newText string, newThought string) error {
	return nil
}

// responseTurn builds a minimal Turn for the batch tier — branch (i) of
// AfterTurn (Response present) — mirroring hook_test.go's responseTurn shape.
func responseTurn(index int, sessionID, text string) *orchestrator.Turn {
	return &orchestrator.Turn{
		Index:     index,
		SessionID: sessionID,
		Mode:      "coder",
		State: &orchestrator.TurnState{
			Response: &llm.Content{Role: "model", Parts: []*llm.Part{{Text: text}}},
		},
	}
}
