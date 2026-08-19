// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// detachedTimeout bounds every MCP call made from the hook. AfterTurn is
// synchronous on ExecuteTurn's return path and the turn's own ctx is dead on
// cancellation/timeout, so calls use a detached bounded context
// (ADR-068 §2.2).
const detachedTimeout = 3 * time.Second

// learnFramePattern matches documented correction frames in the user message
// (full tier only): please remember / please note, from now on, stop <verb>,
// and don't|do not|never|always <imperative>.
var learnFramePattern = regexp.MustCompile(`(?i)\b(please\s+remember\b|please\s+note\b|from\s+now\s+on\b|stop\s+\w+|(?:don't|do\s+not|never|always)\s+\w+)`)

// writeStat tracks attempts and detected failures for one write tool within
// one session (issue #1410 §4). A tool is dead when attempts >= 1 and
// failures == attempts — per-tool all-or-nothing, so partial schema drift on
// any single write tool is caught while transients are tolerated.
type writeStat struct {
	failures int
	attempts int
}

// NewPlurHook creates the Seam B TurnHook. history is the HistoryManager
// port (the same store the context manager uses) — injected at construction
// so branch (iii) GetLastModelTurn is testable without a full sessctx.Manager
// and consistent with accept-interfaces. clk is the injected clock (flock
// poll budget + episode timestamps).
func NewPlurHook(client tools.MCPClient, cfg *atomic.Pointer[config.MemoryConfig], logger ports.Logger, clk clock.Clock, history ports.HistoryManager) orchestrator.TurnHook {
	return &plurHook{
		client:      client,
		cfg:         cfg,
		logger:      logger,
		clock:       clk,
		history:     history,
		buffers:     make(map[string]*sessionBuffer),
		learnCounts: make(map[string]int),
		learnHashes: make(map[string]map[string]struct{}),
		writeStats:  make(map[string]map[string]writeStat),
	}
}

// plurHook captures learnings/episodes after each turn (ADR-068 §2). The
// LEARN tier is mutually exclusive; fail-open everywhere.
type plurHook struct {
	client  tools.MCPClient
	cfg     *atomic.Pointer[config.MemoryConfig]
	logger  ports.Logger
	clock   clock.Clock
	history ports.HistoryManager
	mu      sync.Mutex // guards buffers, lastSessionID/lastIndex, learnCounts, learnHashes, writeStats
	buffers map[string]*sessionBuffer
	// lastSessionID/lastIndex implement belt-and-suspenders turn-scoped
	// dedupe against double-fire (withEngineHook appends, so registration
	// must be once; this is the runtime backstop).
	lastSessionID string
	lastIndex     int
	learnCounts   map[string]int
	learnHashes   map[string]map[string]struct{}
	// writeStats tracks write attempts/failures per (sessionID → tool) —
	// guarded by mu; read once at session end by MemoryWriteReport (which
	// deletes the session's entry). Seam A (injection) carries no counters —
	// injection failures are visible by the absent block, never by this
	// notice.
	writeStats map[string]map[string]writeStat
}

// BeforeTurn is a no-op (interface satisfaction).
func (h *plurHook) BeforeTurn(turn *orchestrator.Turn) {}

// OnPhaseTransition is a no-op (interface satisfaction).
func (h *plurHook) OnPhaseTransition(from, to orchestrator.TurnPhase, state *orchestrator.TurnState) {
}

// AfterTurn classifies the turn outcome and dispatches to the active LEARN
// tier. It returns on every path — memory errors are logged and ignored.
func (h *plurHook) AfterTurn(turn *orchestrator.Turn, err error) {
	cfg := h.cfg.Load()
	if cfg == nil {
		return
	}
	tier := cfg.EffectiveLearnTier()
	if tier == config.MemoryLearnOff {
		return
	}

	// Turn-scoped dedupe (belt-and-suspenders against double-fire).
	h.mu.Lock()
	if turn.SessionID == h.lastSessionID && turn.Index == h.lastIndex {
		h.mu.Unlock()
		return
	}
	h.lastSessionID = turn.SessionID
	h.lastIndex = turn.Index
	h.mu.Unlock()

	// Nil-client runtime guard (hot-reload LEARN != off with a DI-fixed nil
	// client → fail-open no-op).
	if h.client == nil {
		if h.logger != nil {
			h.logger.Warn("memory_client_unavailable", "reason", "nil MCP client", "phase", "learn")
		}
		return
	}

	// Three-way classification keyed on the hook's err argument — never
	// Turn.State.LastError, which is stale on the empty-response retry path.
	var ep episode
	switch {
	case turn.State.Response != nil:
		// (i) Response present (any err): learn from this turn's response.
		text := joinTextParts(turn.State.Response)
		if text == "" {
			return
		}
		ep = episode{Text: text, Mode: turn.Mode, SessionID: turn.SessionID, Timestamp: h.clock.Now()}
		if err != nil {
			ep.Error = err.Error()
		}
	case err != nil:
		// (ii) No response + error: error episode sourced from PreparedHistory
		// (never GetLastModelTurn — it would return the previous turn's
		// response). Transient errors (retry exhaustion) are skipped.
		if orchestrator.IsTransient(err) {
			if h.logger != nil {
				h.logger.Debug("memory_skip_transient_episode", "error", err, "turn", turn.Index)
			}
			return
		}
		ep = episode{
			Error:     err.Error(),
			Prompt:    lastUserText(turn.State.PreparedHistory),
			Mode:      turn.Mode,
			SessionID: turn.SessionID,
			Timestamp: h.clock.Now(),
		}
	default:
		// (iii) No response, no error: the phase loop exited cleanly, so
		// GetLastModelTurn is provably this turn's response. The text-parts
		// filter suppresses intermediate tool-iteration turns.
		detachedCtx, cancel := context.WithTimeout(context.Background(), detachedTimeout)
		_, content, gerr := h.history.GetLastModelTurn(detachedCtx)
		cancel()
		if gerr != nil || content == nil {
			if h.logger != nil {
				h.logger.Warn("memory_get_last_model_turn_failed", "error", gerr, "turn", turn.Index)
			}
			return
		}
		text := joinTextParts(content)
		if text == "" {
			return
		}
		ep = episode{Text: text, Mode: turn.Mode, SessionID: turn.SessionID, Timestamp: h.clock.Now()}
	}

	// Tier dispatch (mutually exclusive; default batch).
	switch tier {
	case config.MemoryLearnCapture:
		h.capture(turn, ep)
	case config.MemoryLearnBatch:
		h.bufferAppend(turn.SessionID, ep)
	case config.MemoryLearnFull:
		h.bufferAppend(turn.SessionID, ep)
		h.maybeLearn(turn, cfg)
	}
}

// capture writes a single episode via plur_capture (capture tier). The
// payload is exactly {summary, agent, session_id} — matching the real
// @plur-ai/mcp schema; text/error/prompt are gone (issue #1410). Fail-open:
// errors are logged and ignored.
func (h *plurHook) capture(turn *orchestrator.Turn, ep episode) {
	args := map[string]interface{}{
		"summary":    buildCaptureSummary(ep),
		"agent":      turn.Mode,
		"session_id": turn.SessionID,
	}

	detachedCtx, cancel := context.WithTimeout(context.Background(), detachedTimeout)
	defer cancel()

	release, ok := acquireWriteLock(h.clock)
	if ok {
		defer release()
	}
	result, err := h.client.CallTool(detachedCtx, "plur_capture", args)
	// Detection: the domain outcome, not the transport error — the MCP
	// adapter surfaces isError rejections as ToolResult.Error with a nil Go
	// error, which the old `err != nil` check never saw (issue #1410).
	if err != nil || result.Error != nil {
		if h.logger != nil {
			h.logger.Warn("memory_capture_failed", "error", firstNonNil(err, result.Error), "turn", turn.Index)
		}
	}
	h.recordWrite(turn.SessionID, "plur_capture", result, err)
}

// bufferAppend appends an episode to the per-session ring buffer (batch and
// full tiers). No MCP call.
func (h *plurHook) bufferAppend(sessionID string, ep episode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	buf, ok := h.buffers[sessionID]
	if !ok {
		buf = &sessionBuffer{}
		h.buffers[sessionID] = buf
	}
	buf.append(ep)
}

// maybeLearn runs the gated direct learn (full tier only): signal is the
// user message only; the matcher is the correction-frame regex; flood bounds
// are MAX_LEARNS_PER_SESSION plus per-session sha256 exact-match dedupe.
// plur_ingest is never auto-fired.
func (h *plurHook) maybeLearn(turn *orchestrator.Turn, cfg *config.MemoryConfig) {
	signal := lastUserText(turn.State.PreparedHistory)
	if !learnFramePattern.MatchString(signal) {
		return
	}
	statement := strings.TrimSpace(signal)
	hash := sha256.Sum256([]byte(strings.ToLower(statement)))
	hashKey := fmt.Sprintf("%x", hash[:])

	h.mu.Lock()
	if h.learnHashes[turn.SessionID] == nil {
		h.learnHashes[turn.SessionID] = make(map[string]struct{})
	}
	if _, dup := h.learnHashes[turn.SessionID][hashKey]; dup {
		h.mu.Unlock()
		return
	}
	if h.learnCounts[turn.SessionID] >= cfg.MaxLearnsPerSession {
		if h.logger != nil {
			h.logger.Debug("memory_learn_flood_bound", "session", turn.SessionID, "limit", cfg.MaxLearnsPerSession)
		}
		h.mu.Unlock()
		return
	}
	h.learnHashes[turn.SessionID][hashKey] = struct{}{}
	h.learnCounts[turn.SessionID]++
	h.mu.Unlock()

	args := map[string]interface{}{
		"statement": statement,
		// Identity rides on the searchable tags convention — never session_id
		// (an unstarted session's session_id risks scope mis-resolution; zero
		// plur_session_start repo-wide) and never agent (not a real parameter),
		// per issue #1410.
		"tags": []string{"session:" + turn.SessionID, "mode:" + turn.Mode},
	}
	if strings.TrimSpace(cfg.Scope) != "" {
		args["scope"] = cfg.Scope
	}

	detachedCtx, cancel := context.WithTimeout(context.Background(), detachedTimeout)
	defer cancel()

	release, ok := acquireWriteLock(h.clock)
	if ok {
		defer release()
	}
	result, err := h.client.CallTool(detachedCtx, "plur_learn", args)
	if err != nil || result.Error != nil {
		if h.logger != nil {
			h.logger.Warn("memory_learn_failed", "error", firstNonNil(err, result.Error), "turn", turn.Index)
		}
	}
	h.recordWrite(turn.SessionID, "plur_learn", result, err)
}

// FlushSession drains the per-session ring buffer and writes it via
// plur_learn_batch (batch and full tiers; called via defer in Chat — success
// and error). Claim: snapshot AND remove the buffered episodes under one
// lock, so a concurrent flush can never double-write the same episodes. The
// MCP call goes outside the lock — never hold the lock across I/O. On write
// failure the claimed episodes are restored (retained for the next flush
// opportunity) and ring-bound drops are reported on the failure Warn —
// fail-open means log-and-move-on, never delete-then-fail (issue #1412).
func (h *plurHook) FlushSession(sessionID string) {
	// Claim: snapshot AND remove the buffered episodes under one lock, so a
	// concurrent flush can never double-write the same episodes (issue
	// #1412). The claim is restored on write failure (retained for the next
	// flush opportunity); on success it stays removed. The MCP call goes
	// outside the lock — never hold the lock across I/O.
	h.mu.Lock()
	buf, ok := h.buffers[sessionID]
	if !ok {
		h.mu.Unlock()
		return
	}
	episodes := buf.claim()
	if len(episodes) == 0 {
		delete(h.buffers, sessionID)
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	// Nil-client guard: permanent misconfiguration — the claim is NOT
	// restored (retention can never succeed; drain-and-drop keeps today's
	// fail-open no-op and bounds map growth). ADR-068 §5.
	if h.client == nil {
		h.mu.Lock()
		delete(h.buffers, sessionID)
		h.mu.Unlock()
		if h.logger != nil {
			h.logger.Warn("memory_client_unavailable", "reason", "nil MCP client", "phase", "learn_batch")
		}
		return
	}

	// Native per-engram scope (never silently dropped) — read the shared
	// hot-reloadable config; nil cfg fails open with no scope.
	var scope string
	if cfg := h.cfg.Load(); cfg != nil {
		scope = strings.TrimSpace(cfg.Scope)
	}
	engrams := make([]engramPayload, 0, len(episodes))
	for _, ep := range episodes {
		e := engramPayload{
			Statement: ep.Text, // non-empty by skip-at-append (T1)
			Tags:      []string{"session:" + ep.SessionID, "mode:" + ep.Mode},
		}
		if scope != "" {
			e.Scope = scope
		}
		engrams = append(engrams, e)
	}
	if len(engrams) == 0 { // belt-and-suspenders — never send engrams: []
		return
	}
	payload := map[string]interface{}{"engrams": engrams, "max_llm_calls": 0}

	detachedCtx, cancel := context.WithTimeout(context.Background(), detachedTimeout)
	defer cancel()

	release, ok := acquireWriteLock(h.clock)
	if ok {
		defer release()
	}
	result, err := h.client.CallTool(detachedCtx, "plur_learn_batch", payload)
	if err != nil || result.Error != nil {
		// Retain on failure: restore the claimed episodes at the front
		// (newer appends stay after them) and report ring-bound drops.
		h.mu.Lock()
		b, exists := h.buffers[sessionID]
		if !exists {
			b = &sessionBuffer{}
			h.buffers[sessionID] = b
		}
		b.restore(episodes)
		dropped := b.dropped
		h.mu.Unlock()
		if h.logger != nil {
			h.logger.Warn("memory_learn_batch_failed", "error", firstNonNil(err, result.Error), "session", sessionID, "retained", len(episodes), "dropped", dropped)
		}
		h.recordWrite(sessionID, "plur_learn_batch", result, err)
		return
	}
	// Success: the claimed episodes were written. Drop the entry when the
	// buffer is empty (episodes appended during the call survive); otherwise
	// the retention window closed, so the drop counter resets.
	h.mu.Lock()
	if b, exists := h.buffers[sessionID]; exists {
		if len(b.episodes) == 0 {
			delete(h.buffers, sessionID)
		} else {
			b.dropped = 0
		}
	}
	h.mu.Unlock()
	h.recordWrite(sessionID, "plur_learn_batch", result, err)
}

// recordWrite increments attempts (always) and failures (on detection) for
// (sessionID, tool). Detection is the domain outcome — err != nil OR
// result.Error != nil — because the MCP adapter surfaces isError rejections
// as ToolResult.Error with a nil Go error (issue #1410). Callers hold no
// lock; never call MCP under this method.
func (h *plurHook) recordWrite(sessionID, tool string, result tools.ToolResult, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	stats := h.writeStats[sessionID]
	if stats == nil {
		stats = make(map[string]writeStat)
		h.writeStats[sessionID] = stats
	}
	st := stats[tool]
	st.attempts++
	if err != nil || result.Error != nil {
		st.failures++
	}
	stats[tool] = st
}

// MemoryWriteReport returns the session-end failure summary for sessionID,
// or "" when no failures were recorded. It names every dead tool (attempts
// >= 1 && failures == attempts — per-tool all-or-nothing) and, because it is
// read exactly once at session end by Chat's defer, deletes the session's
// stats (bounded map growth; the next session starts fresh). Deterministic
// output: dead tools are sorted.
func (h *plurHook) MemoryWriteReport(sessionID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	stats, ok := h.writeStats[sessionID]
	if !ok {
		return ""
	}
	defer delete(h.writeStats, sessionID)

	var totalFailures int
	var dead []string
	for tool, st := range stats {
		totalFailures += st.failures
		if st.attempts >= 1 && st.failures == st.attempts {
			dead = append(dead, tool)
		}
	}
	if totalFailures == 0 {
		return ""
	}
	sort.Strings(dead)
	var sb strings.Builder
	fmt.Fprintf(&sb, "memory write failures: %d", totalFailures)
	for _, tool := range dead {
		fmt.Fprintf(&sb, "; %s failing — learning is disabled", tool)
	}
	return sb.String()
}

// firstNonNil returns err when set, else result.Error — the Warn argument at
// every write site (transport errors win; a nil Go error with result.Error
// set is the adapter's isError rejection surface, issue #1410).
func firstNonNil(err error, resultErr error) error {
	if err != nil {
		return err
	}
	return resultErr
}

// compile-time interface assertion.
var _ orchestrator.TurnHook = (*plurHook)(nil)
