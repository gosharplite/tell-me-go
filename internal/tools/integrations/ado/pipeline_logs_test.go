// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSendScannerHeartbeat(t *testing.T) {
	tests := []struct {
		name         string
		hbSetup      func() chan struct{}
		lineCount    int
		wantReceived bool
	}{
		{
			name: "heartbeat sent on 1000th line",
			hbSetup: func() chan struct{} {
				return make(chan struct{}, 1)
			},
			lineCount:    1000,
			wantReceived: true,
		},
		{
			name: "no heartbeat on 999th line",
			hbSetup: func() chan struct{} {
				return make(chan struct{}, 1)
			},
			lineCount:    999,
			wantReceived: false,
		},
		{
			name: "nil channel does not panic",
			hbSetup: func() chan struct{} {
				return nil
			},
			lineCount:    1000,
			wantReceived: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hb := tt.hbSetup()
			sendScannerHeartbeat(hb, tt.lineCount)
			if tt.wantReceived {
				assert.Equal(t, 1, len(hb), "expected heartbeat to be sent")
			} else if hb != nil {
				assert.Equal(t, 0, len(hb), "expected no heartbeat to be sent")
			}
			// nil channel case: nothing to assert, just verify no panic
		})
	}
}

func TestNewLogFilterState(t *testing.T) {
	tests := []struct {
		name             string
		contextLines     int
		wantContextLines int
		wantPreWinNil    bool
		wantPreWinLen    int
	}{
		{
			name:             "Default context lines (5)",
			contextLines:     5,
			wantContextLines: 5,
			wantPreWinNil:    false,
			wantPreWinLen:    5,
		},
		{
			name:             "Zero context lines",
			contextLines:     0,
			wantContextLines: 0,
			wantPreWinNil:    true,
			wantPreWinLen:    0,
		},
		{
			name:             "Negative context lines defaults to 5",
			contextLines:     -1,
			wantContextLines: 5,
			wantPreWinNil:    false,
			wantPreWinLen:    5,
		},
		{
			name:             "Context lines at boundary 100",
			contextLines:     100,
			wantContextLines: 100,
			wantPreWinNil:    false,
			wantPreWinLen:    100,
		},
		{
			name:             "Context lines exceeds 100 clamps to 100",
			contextLines:     150,
			wantContextLines: 100,
			wantPreWinNil:    false,
			wantPreWinLen:    100,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := newLogFilterState(tt.contextLines)
			assert.Equal(t, tt.wantContextLines, state.contextLines)
			if tt.wantPreWinNil {
				assert.Nil(t, state.preWindow)
			} else {
				assert.Len(t, state.preWindow, tt.wantPreWinLen)
			}
		})
	}
}

func TestStreamFunctions_ContextCancellation(t *testing.T) {
	t.Run("streamTail - context cancelled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := streamTail(ctx, strings.NewReader("a\nb\n"), 10, nil)
		assert.Error(t, err)
	})

	t.Run("streamPagination - context cancelled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := streamPagination(ctx, strings.NewReader("a\nb\n"), 1, 10, nil)
		assert.Error(t, err)
	})

	t.Run("streamRegexFilter - context cancelled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		opts := logFilterOptions{MaxLines: 100, ContextLines: 0}
		_, err := streamRegexFilter(ctx, strings.NewReader("a\nb\n"), "a", opts, nil)
		assert.Error(t, err)
	})

	t.Run("streamTail - buffer overflow", func(t *testing.T) {
		t.Parallel()
		tooLong := strings.NewReader(strings.Repeat("A", 1<<20+1) + "\n")
		_, err := streamTail(context.Background(), tooLong, 10, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
	})

	t.Run("streamPagination - buffer overflow", func(t *testing.T) {
		t.Parallel()
		tooLong := strings.NewReader(strings.Repeat("A", 1<<20+1) + "\n")
		_, err := streamPagination(context.Background(), tooLong, 1, 10, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
	})

	t.Run("streamRegexFilter - buffer overflow", func(t *testing.T) {
		t.Parallel()
		tooLong := strings.NewReader(strings.Repeat("A", 1<<20+1) + "\n")
		opts := logFilterOptions{MaxLines: 100, ContextLines: 0}
		_, err := streamRegexFilter(context.Background(), tooLong, "A", opts, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
	})
}

func TestStreamFunctions_EdgeCases(t *testing.T) {
	t.Run("streamRegexFilter - invalid regex", func(t *testing.T) {
		t.Parallel()
		opts := logFilterOptions{MaxLines: 100, ContextLines: 0}
		_, err := streamRegexFilter(context.Background(), strings.NewReader("data"), "[", opts, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid filter_query regex")
	})

	t.Run("streamTail - n is zero", func(t *testing.T) {
		t.Parallel()
		result, err := streamTail(context.Background(), strings.NewReader("data"), 0, nil)
		assert.NoError(t, err)
		assert.Equal(t, "", result.Content)
	})

	t.Run("streamTail - empty reader", func(t *testing.T) {
		t.Parallel()
		result, err := streamTail(context.Background(), strings.NewReader(""), 10, nil)
		assert.NoError(t, err)
		assert.Equal(t, "", result.Content)
	})

	t.Run("scanLog - processFn error", func(t *testing.T) {
		t.Parallel()
		expectedErr := fmt.Errorf("process error")
		count, err := scanLog(context.Background(), strings.NewReader("line1\nline2\n"), nil, func(line string) (bool, error) {
			return false, expectedErr
		})
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Equal(t, 1, count) // first line triggers error
	})

	t.Run("streamPagination - startLine beyond total", func(t *testing.T) {
		t.Parallel()
		result, err := streamPagination(context.Background(), strings.NewReader("line1\nline2\n"), 10, 10, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Content, "Start line 10 is beyond total lines 2")
	})

	t.Run("streamPagination - maxLines unlimited", func(t *testing.T) {
		t.Parallel()
		result, err := streamPagination(context.Background(), strings.NewReader("line1\nline2\n"), 1, 0, nil)
		assert.NoError(t, err)
		assert.Equal(t, "line1\nline2", result.Content)
		assert.Equal(t, 2, result.TotalLines)
	})

	t.Run("streamRegexFilter - no matches found", func(t *testing.T) {
		t.Parallel()
		opts := logFilterOptions{MaxLines: 100, ContextLines: 0}
		result, err := streamRegexFilter(context.Background(), strings.NewReader("line1\nline2\n"), "zzz_not_present", opts, nil)
		assert.NoError(t, err)
		assert.Equal(t, "No matches found for filter_query.", result.Content)
	})
}
