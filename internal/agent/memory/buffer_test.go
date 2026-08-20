// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package memory

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildCaptureSummary(t *testing.T) {
	// 1001 two-byte runes = 2002 bytes; truncation must land at 2000 bytes
	// (1000 runes), never mid-rune.
	multibyte := strings.Repeat("é", maxEpisodeBytes/2+1)

	tests := []struct {
		name string
		ep   episode
		want string
	}{
		{
			name: "text branch short text returned verbatim",
			ep:   episode{Text: "fix the memory path"},
			want: "fix the memory path",
		},
		{
			name: "text branch long text truncated to maxEpisodeBytes",
			ep:   episode{Text: strings.Repeat("x", maxEpisodeBytes+500)},
			want: strings.Repeat("x", maxEpisodeBytes),
		},
		{
			name: "text branch multibyte runes never split",
			ep:   episode{Text: multibyte},
			want: strings.Repeat("é", maxEpisodeBytes/2),
		},
		{
			name: "text wins over error annotation branch i with err",
			ep:   episode{Text: "response text", Error: "boom"},
			want: "response text",
		},
		{
			name: "error only no fold no prompt",
			ep:   episode{Error: "boom"},
			want: "error: boom",
		},
		{
			name: "error plus prompt fits folded in",
			ep:   episode{Error: "boom", Prompt: "fix the bug"},
			want: "error: boom | user: fix the bug",
		},
		{
			name: "error plus long prompt truncated to remaining budget",
			ep:   episode{Error: "boom", Prompt: strings.Repeat("p", maxEpisodeBytes)},
			want: "error: boom | user: " + strings.Repeat("p", maxEpisodeBytes-len("error: boom | user: ")),
		},
		{
			name: "error base exactly at bound no fold",
			ep:   episode{Error: strings.Repeat("e", maxEpisodeBytes-len("error: ")), Prompt: "ignored"},
			want: "error: " + strings.Repeat("e", maxEpisodeBytes-len("error: ")),
		},
		{
			name: "error base over bound truncated no fold",
			ep:   episode{Error: strings.Repeat("e", maxEpisodeBytes), Prompt: "ignored"},
			want: "error: " + strings.Repeat("e", maxEpisodeBytes-len("error: ")),
		},
		{
			name: "error with empty prompt no fold no trailing separator",
			ep:   episode{Error: "boom", Prompt: ""},
			want: "error: boom",
		},
		{
			name: "empty episode yields empty summary",
			ep:   episode{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCaptureSummary(tt.ep)
			if got != tt.want {
				t.Errorf("buildCaptureSummary(%+v) = %q, want %q", tt.ep, got, tt.want)
			}
			if len(got) > maxEpisodeBytes {
				t.Errorf("summary bytes = %d, want <= %d", len(got), maxEpisodeBytes)
			}
			if !utf8.ValidString(got) {
				t.Errorf("summary is not valid UTF-8: %q", got)
			}
		})
	}
}

func TestSessionBufferAppendSkipsEmptyAndWhitespaceText(t *testing.T) {
	b := &sessionBuffer{}
	b.append(episode{Text: ""})
	b.append(episode{Text: "   "})
	b.append(episode{Text: "\t\n"})
	b.append(episode{Text: "hello"})

	if len(b.episodes) != 1 {
		t.Fatalf("episodes = %d, want 1 (skipped episodes must not occupy ring slots)", len(b.episodes))
	}
	if got := b.episodes[0].Text; got != "hello" {
		t.Errorf("episode text = %q, want %q", got, "hello")
	}
	if b.bytes != len("hello") {
		t.Errorf("bytes = %d, want %d (skipped episodes add nothing)", b.bytes, len("hello"))
	}
}

func TestSessionBufferAppendErrorFloodCannotEvictText(t *testing.T) {
	// The zero-engram case from issue #1410: 5 learnable text episodes, then
	// 25 error-only episodes. Error-only episodes are skipped at append, so
	// the 5 texts must survive with no eviction.
	b := &sessionBuffer{}
	texts := []string{"ep-0", "ep-1", "ep-2", "ep-3", "ep-4"}
	for _, txt := range texts {
		b.append(episode{Text: txt})
	}
	for i := 0; i < 25; i++ {
		b.append(episode{Error: "boom"})
	}

	if len(b.episodes) != 5 {
		t.Fatalf("episodes = %d, want 5", len(b.episodes))
	}
	for i, txt := range texts {
		if got := b.episodes[i].Text; got != txt {
			t.Errorf("episodes[%d] = %q, want %q", i, got, txt)
		}
	}
	wantBytes := 0
	for _, txt := range texts {
		wantBytes += len(txt)
	}
	if b.bytes != wantBytes {
		t.Errorf("bytes = %d, want %d", b.bytes, wantBytes)
	}
}

func TestSessionBufferAppendErrorThenText(t *testing.T) {
	// Reverse ordering: errors flood first (all skipped), then 5 texts.
	b := &sessionBuffer{}
	for i := 0; i < 25; i++ {
		b.append(episode{Error: "boom"})
	}
	for i := 0; i < 5; i++ {
		b.append(episode{Text: fmt.Sprintf("ep-%d", i)})
	}

	if len(b.episodes) != 5 {
		t.Fatalf("episodes = %d, want 5", len(b.episodes))
	}
	if got := b.episodes[0].Text; got != "ep-0" {
		t.Errorf("oldest episode = %q, want %q", got, "ep-0")
	}
	if got := b.episodes[len(b.episodes)-1].Text; got != "ep-4" {
		t.Errorf("newest episode = %q, want %q", got, "ep-4")
	}
}

func TestSessionBufferRestorePrepends(t *testing.T) {
	// Retain-on-failure ordering contract (issue #1412): claimed episodes
	// come back at the front, newer appends stay after them, and bytes are
	// recomputed from the merged set.
	b := &sessionBuffer{}
	b.append(episode{Text: "claimed0"})
	b.append(episode{Text: "claimed1"})
	claimed := b.claim()
	b.append(episode{Text: "newer"})
	b.restore(claimed)

	if len(b.episodes) != 3 || b.episodes[0].Text != "claimed0" || b.episodes[1].Text != "claimed1" || b.episodes[2].Text != "newer" {
		t.Fatalf("episodes after restore = %v, want [claimed0 claimed1 newer]", b.episodes)
	}
	wantBytes := len("claimed0") + len("claimed1") + len("newer")
	if b.bytes != wantBytes {
		t.Errorf("bytes = %d, want %d (sum of merged text lengths)", b.bytes, wantBytes)
	}
}

// TestTruncateToBytes pins truncateToBytes across all four branches
// (buffer.go:142-153): a non-positive max yields "" (buffer.go:143-145),
// len(s) <= max returns s unchanged, a boundary landing mid-rune walks back
// to the rune start (multi-iteration walk-back forced with 3-byte and
// 4-byte runes — the existing buffer test's 2-byte é can land exactly on a
// rune start), and a boundary landing exactly on a rune start needs no
// walk-back. Every result is asserted valid UTF-8, byte length <= max, and
// rune-boundary-terminated (ADR-036 deterministic; hand-rolled, ADR-021).
func TestTruncateToBytes(t *testing.T) {
	// 𝄞 (U+1D11E) is 4 bytes in UTF-8; "x𝄞y" is 6 bytes with the rune
	// spanning bytes 1-4, so max=4 lands on its 4th byte → 3 walk-back
	// iterations to reach the rune start at byte 1.
	fourByte := "x" + "\U0001D11E" + "y"
	// € (U+20AC) is 3 bytes in UTF-8; "a€€b" is 8 bytes with the second €
	// spanning bytes 4-6, so max=6 lands on its 3rd byte → 2 walk-back
	// iterations to reach the rune start at byte 4.
	threeByte := "a" + "\u20ac" + "\u20ac" + "b"

	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"max zero yields empty", "hello", 0, ""},
		{"max negative yields empty", "hello", -1, ""},
		{"len within max returns s unchanged", "hello", 10, "hello"},
		{"ascii boundary exactly on rune start no walk-back", "hello", 3, "hel"},
		{"2-byte rune boundary exactly on rune start no walk-back", "h\u00e9llo", 3, "h\u00e9"},
		{"boundary mid-4-byte-rune walks back three times", fourByte, 4, "x"},
		{"boundary mid-3-byte-rune walks back twice", threeByte, 6, "a\u20ac"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToBytes(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("truncateToBytes(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateToBytes(%q, %d) = %q is not valid UTF-8", tt.s, tt.max, got)
			}
			if tt.max > 0 && len(got) > tt.max {
				t.Errorf("truncateToBytes(%q, %d) byte length = %d, want <= %d", tt.s, tt.max, len(got), tt.max)
			}
			if len(got) > 0 {
				if r, size := utf8.DecodeLastRuneInString(got); r == utf8.RuneError && size == 1 {
					t.Errorf("truncateToBytes(%q, %d) = %q does not end on a rune boundary", tt.s, tt.max, got)
				}
			}
		})
	}
}
