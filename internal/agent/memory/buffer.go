// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"time"
	"unicode/utf8"
)

const (
	// maxBufferEpisodes bounds the per-session ring buffer in count
	// (ADR-068 §3, ~20 turns).
	maxBufferEpisodes = 20
	// maxEpisodeBytes caps each episode's text in bytes.
	maxEpisodeBytes = 2000
)

// episode is one learnable unit captured by plurHook. The JSON tags define
// the plur_learn_batch wire contract (T7's fake plur server mirrors them).
type episode struct {
	Text      string    `json:"text,omitempty"`
	Error     string    `json:"error,omitempty"`
	Prompt    string    `json:"prompt,omitempty"`
	Mode      string    `json:"agent,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// sessionBuffer is a bounded per-session ring buffer of episodes for the
// batch and full LEARN tiers. All access is serialized by the hook's mutex.
type sessionBuffer struct {
	episodes []episode
	bytes    int
}

// append adds an episode to the buffer, truncating ep.Text to maxEpisodeBytes
// (rune-safe) and evicting the oldest entry when the count exceeds
// maxBufferEpisodes. bytes tracks the sum of text bytes in the buffer.
// Callers must hold the hook's mutex.
func (b *sessionBuffer) append(ep episode) {
	ep.Text = truncateToBytes(ep.Text, maxEpisodeBytes)
	b.episodes = append(b.episodes, ep)
	b.bytes += len(ep.Text)

	for len(b.episodes) > maxBufferEpisodes {
		oldest := b.episodes[0]
		b.episodes = b.episodes[1:]
		b.bytes -= len(oldest.Text)
	}
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
