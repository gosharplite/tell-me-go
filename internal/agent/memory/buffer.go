// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxBufferEpisodes bounds the per-session ring buffer in count
	// (ADR-068 §3, ~20 turns). Skip-at-append keeps error-only and
	// whitespace-only episodes out, so the bound applies to learnable
	// turns only.
	maxBufferEpisodes = 20
	// maxEpisodeBytes caps each learnable unit's text in bytes — the
	// plur_capture summary and the plur_learn_batch/plur_learn statement
	// (ADR-068 §3).
	maxEpisodeBytes = 2000
)

// episode is the internal learnable-unit model captured by plurHook — NOT a
// wire contract. Wire shapes are produced explicitly by the mapping helpers
// (buildCaptureSummary, engramPayload) so the internal model can never
// accidentally define the on-the-wire contract again (issue #1410).
type episode struct {
	Text      string
	Error     string
	Prompt    string
	Mode      string
	SessionID string
	Timestamp time.Time
}

// engramPayload is the plur_learn_batch item / plur_learn wire shape
// ({statement, scope?, tags}) — matching the real @plur-ai/mcp schemas
// (issue #1410).
type engramPayload struct {
	Statement string   `json:"statement"`
	Scope     string   `json:"scope,omitempty"`
	Tags      []string `json:"tags"`
}

// sessionBuffer is a bounded per-session ring buffer of episodes for the
// batch and full LEARN tiers. All access is serialized by the hook's mutex.
type sessionBuffer struct {
	episodes []episode
	bytes    int
	// dropped counts ring-bound evictions (append's oldest-episode eviction)
	// since the buffer's last successful flush. Reported on flush failure so
	// retention loss at the bound is visible (issue #1412).
	dropped int
}

// append adds an episode to the buffer, truncating ep.Text to maxEpisodeBytes
// (rune-safe) and evicting the oldest entry when the count exceeds
// maxBufferEpisodes. bytes tracks the sum of text bytes in the buffer.
// Episodes with empty/whitespace-only text are skipped at append and never
// occupy ring capacity. Callers must hold the hook's mutex.
func (b *sessionBuffer) append(ep episode) {
	// Skip-at-append: error-only episodes (branch ii) and whitespace-only
	// text never occupy ring capacity — the bound is "the last ~20 learnable
	// turns" (issue #1410; joinTextParts filters only p.Text == "", so a
	// " " part would otherwise pass through).
	if strings.TrimSpace(ep.Text) == "" {
		return
	}
	ep.Text = truncateToBytes(ep.Text, maxEpisodeBytes)
	b.episodes = append(b.episodes, ep)
	b.bytes += len(ep.Text)

	for len(b.episodes) > maxBufferEpisodes {
		oldest := b.episodes[0]
		b.episodes = b.episodes[1:]
		b.bytes -= len(oldest.Text)
		b.dropped++
	}
}

// claim returns a copy of the buffered episodes and empties the buffer. The
// map entry survives (empty) until the caller decides: write success keeps
// the removal, failure restores via restore (issue #1412). Callers must hold
// the hook's mutex.
func (b *sessionBuffer) claim() []episode {
	eps := append([]episode(nil), b.episodes...)
	b.episodes = nil
	b.bytes = 0
	return eps
}

// restore prepends claimed episodes at the front (retain-on-failure, issue
// #1412): claimed episodes first, newer appends after, bytes recomputed. The
// ring bound is NOT re-enforced here — subsequent appends evict the oldest
// retained episodes and count them in dropped. Callers must hold the hook's
// mutex.
func (b *sessionBuffer) restore(eps []episode) {
	merged := make([]episode, 0, len(eps)+len(b.episodes))
	merged = append(merged, eps...)
	merged = append(merged, b.episodes...)
	b.episodes = merged
	b.bytes = 0
	for _, e := range merged {
		b.bytes += len(e.Text)
	}
}

// buildCaptureSummary renders the required plur_capture summary from an
// episode, always bounded at maxEpisodeBytes (rune-safe). The discriminator
// is TEXT PRESENCE — the branch encoding from AfterTurn: branches (i)/(iii)
// carry the response text (Error may be annotated too, but the text wins);
// branch (ii) has empty Text, so the error-first fold applies there only
// (issue #1410 / grill A3):
//   - text present: truncateToBytes(Text, maxEpisodeBytes);
//   - text absent + error: "error: <Error>", with " | user: <Prompt>" folded
//     in when the prompt fits in the remaining budget (error always survives);
//   - neither: "" (unreachable via AfterTurn).
func buildCaptureSummary(ep episode) string {
	if ep.Text != "" {
		return truncateToBytes(ep.Text, maxEpisodeBytes)
	}
	if ep.Error == "" {
		return ""
	}
	base := "error: " + ep.Error
	if len(base) > maxEpisodeBytes {
		return truncateToBytes(base, maxEpisodeBytes)
	}
	if ep.Prompt == "" {
		return base
	}
	remaining := maxEpisodeBytes - len(base) - len(" | user: ")
	if remaining <= 0 {
		return base
	}
	return base + " | user: " + truncateToBytes(ep.Prompt, remaining)
}

// truncateToBytes cuts s to at most max bytes without splitting a UTF-8
// rune. max is clamped to len(s); a non-positive max yields "".
func truncateToBytes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
