// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bufio"
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

// verifyStreamProcessorInvariants checks the correctness properties that must
// hold after every call to processLine or appendErr.
func verifyStreamProcessorInvariants(t *testing.T, sp *streamProcessor, sb *strings.Builder, rawLine []byte) {
	t.Helper()

	// (b) Capture must never exceed maxCapture.
	if sb.Len() > sp.maxCapture {
		t.Errorf("sb.Len() = %d exceeds maxCapture = %d", sb.Len(), sp.maxCapture)
	}

	// (c) totalCaptured must equal sb.Len() — they track the same data.
	if *sp.totalCaptured != sb.Len() {
		t.Errorf("*sp.totalCaptured = %d, sb.Len() = %d; want equal", *sp.totalCaptured, sb.Len())
	}

	// (d) Truncation invariant: if the buffer is exactly at maxCapture,
	// the truncated flag must be set — further writes will be dropped.
	if sb.Len() > 0 && sb.Len() == sp.maxCapture && !sp.truncated.Load() {
		t.Errorf("sb.Len() == maxCapture (%d) but truncated is false", sp.maxCapture)
	}

	// (e) Output must be valid UTF-8.
	if !utf8.ValidString(sb.String()) {
		t.Errorf("sb.String() is not valid UTF-8: %q", sb.String())
	}
}

// FuzzStreamProcessor explores the behaviour of streamProcessor.processLine
// and streamProcessor.appendErr with arbitrary byte slices.
func FuzzStreamProcessor(f *testing.F) {
	// Seed corpus — register every []byte seed described in the spec.
	f.Add([]byte("hello world"))
	f.Add([]byte(""))
	f.Add([]byte{0xff, 0xfe, 0xfd})                               // invalid UTF-8
	f.Add([]byte{0x80, 0x80, 0x80})                               // continuation bytes without starter
	f.Add([]byte("before\x00after"))                              // NUL mid-string
	f.Add([]byte("\x00\x00\x00"))                                 // all NULs
	f.Add([]byte("\x1b[31mRED\x1b[0m"))                           // ANSI escape
	f.Add([]byte("😀😀😀😀😀😀😀😀"))                                     // 4-byte emoji (8 × 4 = 32 bytes)
	f.Add([]byte("世世世世世世世世"))                                     // 3-byte CJK (8 × 3 = 24 bytes)
	f.Add([]byte{0x00, 0x01, 0x02, 0x03, 0x04})                   // binary garbage
	f.Add(bytes.Repeat([]byte("A"), 500))                         // long line (exercises truncation)
	f.Add([]byte("progress\r"))                                   // carriage return
	f.Add([]byte("col1\tcol2\tcol3"))                             // tabs
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}) // control chars
	f.Add([]byte("\xef\xbf\xbd"))                                 // Unicode replacement char U+FFFD

	f.Fuzz(func(t *testing.T, rawLine []byte) {
		// 1. Construct streamProcessor per spec.
		sp := &streamProcessor{
			mu:            &sync.Mutex{},
			truncated:     &atomic.Bool{},
			totalCaptured: new(int),
			maxCapture:    256,
			file:          nil,
			feedback:      nil,
			wt:            nil,
			// stdoutStr and stderrStr left as zero-value (nil)
		}

		var sb strings.Builder

		// 2. Process the fuzzer-provided line with empty prefix.
		sp.processLine(&sb, rawLine, "", nil)

		// 3. Verify invariants after processLine.
		verifyStreamProcessorInvariants(t, sp, &sb, rawLine)

		// 4. nil error — must be a true no-op.
		sbLenBefore := sb.Len()
		truncatedBefore := sp.truncated.Load()
		sp.appendErr(&sb, nil)
		if sb.Len() != sbLenBefore {
			t.Errorf("appendErr(nil): sb.Len() changed from %d to %d", sbLenBefore, sb.Len())
		}
		if sp.truncated.Load() != truncatedBefore {
			t.Errorf("appendErr(nil): truncated changed from %v to %v", truncatedBefore, sp.truncated.Load())
		}

		// 5. bufio.ErrTooLong — warning must appear when there is room.
		sbLenBeforeAppend := sb.Len()
		sp.appendErr(&sb, bufio.ErrTooLong)
		if sbLenBeforeAppend < sp.maxCapture {
			// When remaining > 0, appendErr must write at least some content
			// (even if truncated by sanitizeAndTruncateUTF8).
			if sb.Len() <= sbLenBeforeAppend {
				t.Errorf("appendErr(ErrTooLong): sb did not grow (len=%d before, len=%d after) when remaining > 0",
					sbLenBeforeAppend, sb.Len())
			}
		}
		// When sb was already at maxCapture, the truncated flag is re-set
		// (no-op) but no new content appears — that's acceptable.

		// 6. Invariants must still hold after appendErr.
		verifyStreamProcessorInvariants(t, sp, &sb, rawLine)
	})
}
