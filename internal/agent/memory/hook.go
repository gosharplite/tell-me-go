// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
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
	mu      sync.Mutex // guards buffers, lastSessionID/lastIndex, learnCounts, learnHashes
	buffers map[string]*sessionBuffer
	// lastSessionID/lastIndex implement belt-and-suspenders turn-scoped
	// dedupe against double-fire (withEngineHook appends, so registration
	// must be once; this is the runtime backstop).
	lastSessionID string
	lastIndex     int
	learnCounts   map[string]int
	learnHashes   map[string]map[string]struct{}
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

// capture writes a single episode via plur_capture (capture tier). Fail-open:
// errors are logged and ignored.
func (h *plurHook) capture(turn *orchestrator.Turn, ep episode) {
	args := map[string]interface{}{
		"agent":      turn.Mode,
		"session_id": turn.SessionID,
	}
	if ep.Text != "" {
		args["text"] = ep.Text
	}
	if ep.Error != "" {
		args["error"] = ep.Error
	}
	if ep.Prompt != "" {
		args["prompt"] = ep.Prompt
	}

	detachedCtx, cancel := context.WithTimeout(context.Background(), detachedTimeout)
	defer cancel()

	release, ok := acquireWriteLock(h.clock)
	if ok {
		defer release()
	}
	if _, err := h.client.CallTool(detachedCtx, "plur_capture", args); err != nil {
		if h.logger != nil {
			h.logger.Warn("memory_capture_failed", "error", err, "turn", turn.Index)
		}
	}
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
		"agent":     turn.Mode,
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
	if _, err := h.client.CallTool(detachedCtx, "plur_learn", args); err != nil {
		if h.logger != nil {
			h.logger.Warn("memory_learn_failed", "error", err, "turn", turn.Index)
		}
	}
}

// FlushSession drains the per-session ring buffer and writes it via
// plur_learn_batch (batch and full tiers; called via defer in Chat — success
// and error). Drain happens under lock; the MCP call goes outside the lock —
// never hold the lock across I/O.
func (h *plurHook) FlushSession(sessionID string) {
	h.mu.Lock()
	buf, ok := h.buffers[sessionID]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(h.buffers, sessionID)
	episodes := append([]episode(nil), buf.episodes...)
	h.mu.Unlock()

	if len(episodes) == 0 {
		return
	}

	// Nil-client guard: drain happened above (fail-open); skip the call.
	if h.client == nil {
		if h.logger != nil {
			h.logger.Warn("memory_client_unavailable", "reason", "nil MCP client", "phase", "learn_batch")
		}
		return
	}

	payload := map[string]interface{}{
		"episodes":   episodes,
		"session_id": sessionID,
	}

	detachedCtx, cancel := context.WithTimeout(context.Background(), detachedTimeout)
	defer cancel()

	release, ok := acquireWriteLock(h.clock)
	if ok {
		defer release()
	}
	if _, err := h.client.CallTool(detachedCtx, "plur_learn_batch", payload); err != nil {
		if h.logger != nil {
			h.logger.Warn("memory_learn_batch_failed", "error", err, "session", sessionID)
		}
	}
}

// compile-time interface assertion.
var _ orchestrator.TurnHook = (*plurHook)(nil)
