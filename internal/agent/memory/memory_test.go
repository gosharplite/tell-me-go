// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// mockMCPClient is a hand-rolled function-field test double for
// tools.MCPClient (ADR-021/036 pattern): methods lock → append call record →
// unlock, then invoke the user Func OUTSIDE the lock (non-reentrant deadlock
// hazard). The zero value is usable — CallTool returns (zero, nil) and
// records every call.
type mockMCPClient struct {
	mu            sync.Mutex
	ListToolsFunc func(ctx context.Context) ([]tools.MCPToolDefinition, error)
	CallToolFunc  func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error)
	records       []string
}

// ListTools implements tools.MCPClient.
func (m *mockMCPClient) ListTools(ctx context.Context) ([]tools.MCPToolDefinition, error) {
	m.mu.Lock()
	m.records = append(m.records, "ListTools")
	m.mu.Unlock()
	if m.ListToolsFunc != nil {
		return m.ListToolsFunc(ctx)
	}
	return nil, nil
}

// CallTool implements tools.MCPClient. Record format: "CallTool:<name>".
func (m *mockMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	m.mu.Lock()
	m.records = append(m.records, "CallTool:"+name)
	m.mu.Unlock()
	if m.CallToolFunc != nil {
		return m.CallToolFunc(ctx, name, args)
	}
	return tools.ToolResult{}, nil
}

// Close implements tools.MCPClient.
func (m *mockMCPClient) Close() error { return nil }

// recordedNames returns a race-safe snapshot of the call records.
func (m *mockMCPClient) recordedNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.records))
	copy(out, m.records)
	return out
}

// stubHistoryManager implements ports.HistoryManager (16 methods — hand-rolled
// per the interface-satisfying-stub acceptance class). Only GetLastModelTurn
// is meaningful; the rest return zero values. Deliberately NOT
// agenttest.MockHistoryManager: agenttest imports agent, which will import
// memory in the wiring task (a test-only import cycle).
type stubHistoryManager struct {
	GetLastModelTurnFunc func(ctx context.Context) (int, *llm.Content, error)
}

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
	if s.GetLastModelTurnFunc != nil {
		return s.GetLastModelTurnFunc(ctx)
	}
	return 0, nil, nil
}

func (s *stubHistoryManager) GetModelTurn(ctx context.Context, index int) (*llm.Content, error) {
	return nil, nil
}

func (s *stubHistoryManager) UpdateTurnContent(ctx context.Context, index int, newText string, newThought string) error {
	return nil
}

// newTestInjector builds a plurInjector with a DefaultMemoryConfig (budget
// 2000, batch tier, flood bound 3) and the given enabled state.
func newTestInjector(t *testing.T, client tools.MCPClient, enabled bool) (*plurInjector, *atomic.Pointer[config.MemoryConfig]) {
	t.Helper()
	memCfg := config.DefaultMemoryConfig()
	memCfg.Enabled = enabled
	cfgPtr := &atomic.Pointer[config.MemoryConfig]{}
	cfgPtr.Store(&memCfg)
	return NewPlurInjector(client, cfgPtr, &ports.NoOpLogger{}).(*plurInjector), cfgPtr
}

// newTestHook builds a plurHook with a DefaultMemoryConfig and the given
// tier. HOME is redirected to a temp dir so flock side effects never touch
// the developer's real ~/.plur. clk is a FakeClock (deterministic Now()).
func newTestHook(t *testing.T, client tools.MCPClient, tier config.MemoryLearnTier, hist ports.HistoryManager) (*plurHook, *atomic.Pointer[config.MemoryConfig]) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	memCfg := config.DefaultMemoryConfig()
	memCfg.Enabled = true
	memCfg.LearnTier = tier
	cfgPtr := &atomic.Pointer[config.MemoryConfig]{}
	cfgPtr.Store(&memCfg)
	return NewPlurHook(client, cfgPtr, &ports.NoOpLogger{}, &clock.FakeClock{}, hist).(*plurHook), cfgPtr
}

// recall returns a CallToolFunc that always returns the given recall text.
func recall(text string) func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: text}, nil
	}
}

// countMarkerParts counts Parts whose Text contains the memory marker.
func countMarkerParts(history []*llm.Content) int {
	n := 0
	for _, c := range history {
		for _, p := range c.Parts {
			if p != nil && strings.Contains(p.Text, memoryMarker) {
				n++
			}
		}
	}
	return n
}

func TestSessionBufferAppendTruncatesLongText(t *testing.T) {
	b := &sessionBuffer{}
	ep := episode{Text: strings.Repeat("x", maxEpisodeBytes+1000), Timestamp: time.Unix(0, 0)}
	b.append(ep)
	if len(b.episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(b.episodes))
	}
	if got := len(b.episodes[0].Text); got != maxEpisodeBytes {
		t.Errorf("text length = %d, want %d", got, maxEpisodeBytes)
	}
	if b.bytes != maxEpisodeBytes {
		t.Errorf("bytes = %d, want %d", b.bytes, maxEpisodeBytes)
	}
}

func TestSessionBufferAppendTruncatesAtRuneBoundary(t *testing.T) {
	b := &sessionBuffer{}
	// 1000 two-byte runes = 2000 bytes exactly (fits); add 1 to exceed.
	text := strings.Repeat("é", maxEpisodeBytes/2+1)
	b.append(episode{Text: text, Timestamp: time.Unix(0, 0)})
	if got := len(b.episodes[0].Text); got > maxEpisodeBytes {
		t.Errorf("text bytes = %d, want <= %d", got, maxEpisodeBytes)
	}
	// The truncated text must not end mid-rune (all é are 2 bytes).
	if got := len(b.episodes[0].Text); got%2 != 0 {
		t.Errorf("text ends mid-rune: byte length %d not divisible by 2", got)
	}
}

func TestSessionBufferAppendEvictsOldest(t *testing.T) {
	b := &sessionBuffer{}
	for i := 0; i < maxBufferEpisodes+5; i++ {
		b.append(episode{Text: fmt.Sprintf("ep-%d", i), Timestamp: time.Unix(0, 0)})
	}
	if len(b.episodes) != maxBufferEpisodes {
		t.Fatalf("episodes = %d, want %d", len(b.episodes), maxBufferEpisodes)
	}
	if got := b.episodes[0].Text; got != "ep-5" {
		t.Errorf("first (oldest) episode = %q, want %q", got, "ep-5")
	}
	if got := b.episodes[len(b.episodes)-1].Text; got != fmt.Sprintf("ep-%d", maxBufferEpisodes+4) {
		t.Errorf("last episode = %q, want %q", got, fmt.Sprintf("ep-%d", maxBufferEpisodes+4))
	}
}

func TestExtractIDs(t *testing.T) {
	tests := []struct {
		name   string
		result tools.ToolResult
		want   []string
	}{
		{
			name:   "metadata string",
			result: tools.ToolResult{Metadata: map[string]interface{}{"ids": "e1"}},
			want:   []string{"e1"},
		},
		{
			name:   "metadata string slice",
			result: tools.ToolResult{Metadata: map[string]interface{}{"ids": []string{"e1", "e2"}}},
			want:   []string{"e1", "e2"},
		},
		{
			name:   "metadata interface slice",
			result: tools.ToolResult{Metadata: map[string]interface{}{"ids": []interface{}{"e1", "e2"}}},
			want:   []string{"e1", "e2"},
		},
		{
			name:   "metadata dedupe order-preserving",
			result: tools.ToolResult{Metadata: map[string]interface{}{"ids": []string{"e1", "e1", "e2"}}},
			want:   []string{"e1", "e2"},
		},
		{
			name:   "regex fallback engram_id and id",
			result: tools.ToolResult{Text: "engram_id: e1\nid: e2"},
			want:   []string{"e1", "e2"},
		},
		{
			name:   "regex equals form",
			result: tools.ToolResult{Text: "id=e1"},
			want:   []string{"e1"},
		},
		{
			name:   "regex dedupe order-preserving",
			result: tools.ToolResult{Text: "id: e1\nid: e1\nid: e2"},
			want:   []string{"e1", "e2"},
		},
		{
			name:   "regex ignores malformed ids",
			result: tools.ToolResult{Text: "id: !!not-a-match!!"},
			want:   nil,
		},
		{
			name:   "no ids",
			result: tools.ToolResult{Text: "plain recall text"},
			want:   nil,
		},
		{
			name:   "empty metadata ids falls back to regex",
			result: tools.ToolResult{Text: "id: e1", Metadata: map[string]interface{}{"ids": []string{}}},
			want:   []string{"e1"},
		},
		{
			name:   "nil metadata",
			result: tools.ToolResult{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIDs(tt.result)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractIDs(%+v) = %v, want %v", tt.result, got, tt.want)
			}
		})
	}
}

func TestAcquireWriteLockSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// OpenFile does not create parent directories; production ~/.plur exists
	// (PLUR's store), so the test mirrors that shape.
	if err := os.MkdirAll(filepath.Join(home, ".plur"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	clk := &clock.FakeClock{}
	release, ok := acquireWriteLock(clk)
	if !ok || release == nil {
		t.Fatal("expected lock acquisition to succeed")
	}
	release()
}

func TestAcquireWriteLockBudgetExhausted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	lockPath := filepath.Join(home, ".plur", ".tmg-write.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("flock holder: %v", err)
	}
	defer func() { _ = syscall.Flock(int(holder.Fd()), syscall.LOCK_UN) }()

	clk := &clock.FakeClock{SleepChan: make(chan time.Duration, flockMaxAttempts)}
	release, ok := acquireWriteLock(clk)
	if ok || release != nil {
		t.Fatal("expected lock acquisition to fail after budget exhaustion")
	}
	if got := len(clk.SleepChan); got != flockMaxAttempts {
		t.Errorf("Sleep calls = %d, want %d", got, flockMaxAttempts)
	}
	select {
	case d := <-clk.SleepChan:
		if d != flockPollInterval {
			t.Errorf("Sleep duration = %v, want %v", d, flockPollInterval)
		}
	default:
		t.Error("SleepChan empty; expected flockPollInterval durations")
	}
}

func TestAcquireWriteLockUncreatable(t *testing.T) {
	// HOME points inside a non-existent directory → OpenFile fails → fail-open.
	t.Setenv("HOME", filepath.Join(t.TempDir(), "missing", "home"))
	release, ok := acquireWriteLock(&clock.FakeClock{})
	if ok || release != nil {
		t.Fatal("expected fail-open (nil, false) when the lock file cannot be created")
	}
}

func TestAcquireWriteLockNilClock(t *testing.T) {
	// Nil clock: EWOULDBLOCK still exhausts the budget without sleeping.
	home := t.TempDir()
	t.Setenv("HOME", home)
	lockPath := filepath.Join(home, ".plur", ".tmg-write.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("flock holder: %v", err)
	}
	defer func() { _ = syscall.Flock(int(holder.Fd()), syscall.LOCK_UN) }()

	release, ok := acquireWriteLock(nil)
	if ok || release != nil {
		t.Fatal("expected fail-open (nil, false) with nil clock")
	}
}
