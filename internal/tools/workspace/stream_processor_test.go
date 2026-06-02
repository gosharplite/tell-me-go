// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// streamProcessor tests
// ---------------------------------------------------------------------------

// verifyAppendErrResult asserts the state of the string builder, truncated flag,
// and optional feedback buffer against the test case expectations.
func verifyAppendErrResult(t *testing.T, sb *strings.Builder, sp *streamProcessor, wantEmptySB bool, wantContains string, wantTruncated bool, useFeedback bool, fb io.Writer) {
	t.Helper()
	got := sb.String()
	if wantEmptySB && got != "" {
		t.Errorf("expected empty sb, got %q", got)
	}
	if wantContains != "" && !strings.Contains(got, wantContains) {
		t.Errorf("expected sb to contain %q, got %q", wantContains, got)
	}
	if sp.truncated.Load() != wantTruncated {
		t.Errorf("truncated = %v, want %v", sp.truncated.Load(), wantTruncated)
	}
	if useFeedback && fb != nil {
		fbStr := fb.(*bytes.Buffer).String()
		if !strings.Contains(fbStr, wantContains) {
			t.Errorf("expected feedback to contain %q, got %q", wantContains, fbStr)
		}
	}
}

func TestStreamProcessor_appendErr(t *testing.T) {
	tests := []struct {
		name          string
		totalCaptured int
		maxCapture    int
		err           error
		wantContains  string
		wantTruncated bool
		wantEmptySB   bool
		useFeedback   bool
	}{
		{
			name:          "nil error — no-op",
			totalCaptured: 0,
			maxCapture:    500,
			err:           nil,
			wantContains:  "",
			wantTruncated: false,
			wantEmptySB:   true,
		},
		{
			name:          "ErrTooLong — warning without error detail",
			totalCaptured: 0,
			maxCapture:    500,
			err:           bufio.ErrTooLong,
			wantContains:  "[Warning] Output line too long",
			wantTruncated: false,
		},
		{
			name:          "generic error — warning with error text",
			totalCaptured: 0,
			maxCapture:    500,
			err:           fmt.Errorf("mock read error"),
			wantContains:  "[Warning] Output read error: mock read error",
			wantTruncated: false,
		},
		{
			name:          "truncated by remaining capacity",
			totalCaptured: 490,
			maxCapture:    500,
			err:           fmt.Errorf("some error"),
			wantContains:  "\n[Warning]",
			wantTruncated: true,
		},
		{
			name:          "already at max — truncated flag set, nothing written",
			totalCaptured: 500,
			maxCapture:    500,
			err:           fmt.Errorf("some error"),
			wantContains:  "",
			wantTruncated: true,
			wantEmptySB:   true,
		},
		{
			name:          "error with feedback writer",
			totalCaptured: 0,
			maxCapture:    500,
			err:           fmt.Errorf("feedback test error"),
			wantContains:  "[Warning] Output read error: feedback test error",
			wantTruncated: false,
			useFeedback:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalCaptured := tt.totalCaptured
			var fb io.Writer
			if tt.useFeedback {
				fb = &bytes.Buffer{}
			}
			sp := &streamProcessor{
				mu:            &sync.Mutex{},
				truncated:     &atomic.Bool{},
				totalCaptured: &totalCaptured,
				maxCapture:    tt.maxCapture,
				feedback:      fb,
			}

			var sb strings.Builder
			sp.appendErr(&sb, tt.err)

			verifyAppendErrResult(t, &sb, sp, tt.wantEmptySB, tt.wantContains, tt.wantTruncated, tt.useFeedback, fb)
		})
	}
}

// TestStreamProcessor_processLine_EdgeCases verifies that processLine handles
// nil file and nil feedback without panicking.
func TestStreamProcessor_processLine_EdgeCases(t *testing.T) {
	t.Run("nil file — line captured in string builder only", func(t *testing.T) {
		mu := &sync.Mutex{}
		truncated := &atomic.Bool{}
		totalCaptured := 0
		sp := &streamProcessor{
			stdoutStr:     &strings.Builder{},
			mu:            mu,
			truncated:     truncated,
			totalCaptured: &totalCaptured,
			maxCapture:    1024,
			file:          nil, // explicitly nil file
			feedback:      nil,
		}

		var sb strings.Builder
		sp.processLine(&sb, []byte("hello"), "", nil)

		if !strings.Contains(sb.String(), "hello") {
			t.Errorf("expected sb to contain 'hello', got %q", sb.String())
		}
	})

	t.Run("nil feedback — no panic, no feedback written", func(t *testing.T) {
		mu := &sync.Mutex{}
		truncated := &atomic.Bool{}
		totalCaptured := 0
		sp := &streamProcessor{
			stdoutStr:     &strings.Builder{},
			mu:            mu,
			truncated:     truncated,
			totalCaptured: &totalCaptured,
			maxCapture:    1024,
			file:          nil,
			feedback:      nil, // explicitly nil feedback
		}

		var sb strings.Builder
		// Should not panic when feedback is nil
		sp.processLine(&sb, []byte("world"), "PREFIX", nil)

		if !strings.Contains(sb.String(), "PREFIX world") {
			t.Errorf("expected sb to contain 'PREFIX world', got %q", sb.String())
		}
	})

	t.Run("nil file with feedback writer — feedback still written", func(t *testing.T) {
		mu := &sync.Mutex{}
		truncated := &atomic.Bool{}
		totalCaptured := 0
		fb := &bytes.Buffer{}
		sp := &streamProcessor{
			stdoutStr:     &strings.Builder{},
			mu:            mu,
			truncated:     truncated,
			totalCaptured: &totalCaptured,
			maxCapture:    1024,
			file:          nil,
			feedback:      fb,
		}

		var sb strings.Builder
		sp.processLine(&sb, []byte("test"), "", fb)

		if !strings.Contains(sb.String(), "test") {
			t.Errorf("expected sb to contain 'test', got %q", sb.String())
		}
		if !strings.Contains(fb.String(), "test") {
			t.Errorf("expected feedback to contain 'test', got %q", fb.String())
		}
	})

	t.Run("nil file with typed nil *os.File", func(t *testing.T) {
		mu := &sync.Mutex{}
		truncated := &atomic.Bool{}
		totalCaptured := 0
		var f *os.File = nil
		sp := &streamProcessor{
			stdoutStr:     &strings.Builder{},
			mu:            mu,
			truncated:     truncated,
			totalCaptured: &totalCaptured,
			maxCapture:    1024,
			file:          f, // typed nil
			feedback:      nil,
		}

		var sb strings.Builder
		// Should not panic — sp.file != nil is false for typed nil
		sp.processLine(&sb, []byte("typed-nil"), "", nil)

		if !strings.Contains(sb.String(), "typed-nil") {
			t.Errorf("expected sb to contain 'typed-nil', got %q", sb.String())
		}
	})
}
