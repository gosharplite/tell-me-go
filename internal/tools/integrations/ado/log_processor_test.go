// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessLogContent(t *testing.T) {
	content := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10"
	ctx := context.Background()

	t.Run("TailLines", func(t *testing.T) {
		result, err := processLogContent(ctx, strings.NewReader(content), 3, 0, "", 0, 0, 0, nil)
		assert.NoError(t, err)
		expected := "line 8\nline 9\nline 10"
		assert.Equal(t, expected, result.Content)
	})

	t.Run("HeadLines", func(t *testing.T) {
		result, err := processLogContent(ctx, strings.NewReader(content), 0, 3, "", 0, 0, 0, nil)
		assert.NoError(t, err)
		expected := "line 1\nline 2\nline 3"
		assert.Equal(t, expected, result.Content)
		assert.True(t, result.Truncated)
	})

	t.Run("DefaultTailLines", func(t *testing.T) {
		// Create content with 250 lines

		var lines []string
		for i := 1; i <= 250; i++ {
			lines = append(lines, "line contents")
		}
		longContent := strings.Join(lines, "\n")
		result, err := processLogContent(ctx, strings.NewReader(longContent), 0, 0, "", 0, 0, 0, nil)
		assert.NoError(t, err)
		resultLines := strings.Split(result.Content, "\n")
		assert.Equal(t, 200, len(resultLines))
		assert.True(t, result.Truncated)
	})

	t.Run("FilterQuery", func(t *testing.T) {
		contentWithErrors := "info: start\nerror: something failed\ninfo: middle\nerror: another failure\ninfo: end"
		result, err := processLogContent(ctx, strings.NewReader(contentWithErrors), 0, 0, "error", 1, 0, 0, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Content, "error: something failed")
		assert.Contains(t, result.Content, "error: another failure")
		assert.Contains(t, result.Content, "info: start")
		assert.Contains(t, result.Content, "info: middle")
		assert.Contains(t, result.Content, "info: end")
	})

	t.Run("FilterQueryWithGaps", func(t *testing.T) {
		contentWithErrors := "line 1\nline 2\nline 3\nerror: match\nline 5\nline 6\nline 7\nline 8\nline 9\nerror: match 2\nline 11\nline 12"
		result, err := processLogContent(ctx, strings.NewReader(contentWithErrors), 0, 0, "error", 1, 0, 0, nil)
		assert.NoError(t, err)
		resultLines := strings.Split(result.Content, "\n")

		assert.Contains(t, result.Content, "error: match")
		assert.Contains(t, result.Content, "error: match 2")
		assert.Contains(t, result.Content, "...")

		// Check that line 1 is not present as a full line
		for _, line := range resultLines {
			assert.NotEqual(t, "line 1", line)
			assert.NotEqual(t, "line 7", line)
		}
	})

	t.Run("Pagination", func(t *testing.T) {
		result, err := processLogContent(ctx, strings.NewReader(content), 0, 0, "", 0, 2, 3, nil)
		assert.NoError(t, err)
		expected := "line 2\nline 3\nline 4"
		assert.Equal(t, expected, result.Content)
		assert.True(t, result.Truncated)
	})

	t.Run("PaginationBeyondEnd", func(t *testing.T) {
		result, err := processLogContent(ctx, strings.NewReader(content), 0, 0, "", 0, 9, 5, nil)
		assert.NoError(t, err)
		expected := "line 9\nline 10"
		assert.Equal(t, expected, result.Content)
		assert.False(t, result.Truncated)
	})

	t.Run("ArgumentPriority", func(t *testing.T) {
		content := "line 1\nerror: critical\nline 3\nline 4\nline 5\nerror: fatal\nline 7"

		// Pass ALL stream manipulators at once.
		// filterQuery="error" should take precedence over startLine, maxLines, headLines, tailLines.
		result, err := processLogContent(ctx, strings.NewReader(content), 2, 2, "error", 0, 5, 2, nil)

		assert.NoError(t, err)
		// It should only return the filtered lines, ignoring the head/tail/pagination logic
		assert.Contains(t, result.Content, "error: critical")
		assert.Contains(t, result.Content, "error: fatal")
		assert.NotContains(t, result.Content, "line 1")
		assert.NotContains(t, result.Content, "line 7")
	})
}

func TestOOMSafety(t *testing.T) {
	ctx := context.Background()

	t.Run("ClampTailLines", func(t *testing.T) {
		content := "line 1\nline 2"
		result, err := streamTail(ctx, strings.NewReader(content), 1000000, nil)
		assert.NoError(t, err)
		assert.Equal(t, "line 1\nline 2", result.Content)
	})

	t.Run("ClampFilterContextLines", func(t *testing.T) {
		content := "match"
		result, err := streamRegexFilter(ctx, strings.NewReader(content), "match", logFilterOptions{ContextLines: 1000000}, nil)
		assert.NoError(t, err)
		assert.Equal(t, "match", result.Content)
	})
}

type logErrorReader struct{}

func (e *logErrorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestLogIOError(t *testing.T) {
	errReader := &logErrorReader{}
	ctx := context.Background()

	t.Run("streamTail", func(t *testing.T) {
		result, err := streamTail(ctx, errReader, 10, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
		assert.Empty(t, result.Content)
	})

	t.Run("streamHead", func(t *testing.T) {
		result, err := streamHead(ctx, errReader, 10, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
		assert.Empty(t, result.Content)
	})

	t.Run("streamPagination", func(t *testing.T) {
		result, err := streamPagination(ctx, errReader, 1, 10, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
		assert.Empty(t, result.Content)
	})

	t.Run("streamRegexFilter", func(t *testing.T) {
		result, err := streamRegexFilter(ctx, errReader, "match", logFilterOptions{ContextLines: 5}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
		assert.Empty(t, result.Content)
	})
}

func TestProcessLogContent_BufferTooLong(t *testing.T) {
	ctx := context.Background()

	// Create a single line of text that exceeds the 1MB bufio.Scanner maxCapacity limit
	massiveLine := strings.Repeat("A", (1*1024*1024)+100)

	result, err := processLogContent(ctx, strings.NewReader(massiveLine), 0, 0, "", 0, 0, 0, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "log stream interrupted")
	assert.Contains(t, err.Error(), "token too long")
	assert.Empty(t, result.Content)
}
