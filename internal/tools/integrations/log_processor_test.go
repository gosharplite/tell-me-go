// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessLogContent(t *testing.T) {
	m := &azureDevOpsManager{}
	content := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10"

	t.Run("TailLines", func(t *testing.T) {
		result, err := m.processLogContent(strings.NewReader(content), 3, 0, "", 0, 0, 0)
		assert.NoError(t, err)
		expected := "line 8\nline 9\nline 10"
		assert.Equal(t, expected, result)
	})

	t.Run("HeadLines", func(t *testing.T) {
		result, err := m.processLogContent(strings.NewReader(content), 0, 3, "", 0, 0, 0)
		assert.NoError(t, err)
		expected := "line 1\nline 2\nline 3"
		assert.Equal(t, expected, result)
	})

	t.Run("DefaultTailLines", func(t *testing.T) {
		// Create content with 250 lines
		var lines []string
		for i := 1; i <= 250; i++ {
			lines = append(lines, "line contents")
		}
		longContent := strings.Join(lines, "\n")
		result, err := m.processLogContent(strings.NewReader(longContent), 0, 0, "", 0, 0, 0)
		assert.NoError(t, err)
		resultLines := strings.Split(result, "\n")
		assert.Equal(t, 200, len(resultLines))
	})

	t.Run("FilterQuery", func(t *testing.T) {
		contentWithErrors := "info: start\nerror: something failed\ninfo: middle\nerror: another failure\ninfo: end"
		result, err := m.processLogContent(strings.NewReader(contentWithErrors), 0, 0, "error", 1, 0, 0)
		assert.NoError(t, err)
		assert.Contains(t, result, "error: something failed")
		assert.Contains(t, result, "error: another failure")
		assert.Contains(t, result, "info: start")
		assert.Contains(t, result, "info: middle")
		assert.Contains(t, result, "info: end")
	})

	t.Run("FilterQueryWithGaps", func(t *testing.T) {
		contentWithErrors := "line 1\nline 2\nline 3\nerror: match\nline 5\nline 6\nline 7\nline 8\nline 9\nerror: match 2\nline 11\nline 12"
		result, err := m.processLogContent(strings.NewReader(contentWithErrors), 0, 0, "error", 1, 0, 0)
		assert.NoError(t, err)
		resultLines := strings.Split(result, "\n")
		
		assert.Contains(t, result, "error: match")
		assert.Contains(t, result, "error: match 2")
		assert.Contains(t, result, "...")
		
		// Check that line 1 is not present as a full line
		for _, line := range resultLines {
			assert.NotEqual(t, "line 1", line)
			assert.NotEqual(t, "line 7", line)
		}
	})

	t.Run("Pagination", func(t *testing.T) {
		result, err := m.processLogContent(strings.NewReader(content), 0, 0, "", 0, 2, 3)
		assert.NoError(t, err)
		expected := "line 2\nline 3\nline 4"
		assert.Equal(t, expected, result)
	})

	t.Run("PaginationBeyondEnd", func(t *testing.T) {
		result, err := m.processLogContent(strings.NewReader(content), 0, 0, "", 0, 9, 5)
		assert.NoError(t, err)
		expected := "line 9\nline 10"
		assert.Equal(t, expected, result)
	})

	t.Run("ArgumentPriority", func(t *testing.T) {
		content := "line 1\nerror: critical\nline 3\nline 4\nline 5\nerror: fatal\nline 7"

		// Pass ALL stream manipulators at once.
		// filterQuery="error" should take precedence over startLine, maxLines, headLines, tailLines.
		result, err := m.processLogContent(strings.NewReader(content), 2, 2, "error", 0, 5, 2)

		assert.NoError(t, err)
		// It should only return the filtered lines, ignoring the head/tail/pagination logic
		assert.Contains(t, result, "error: critical")
		assert.Contains(t, result, "error: fatal")
		assert.NotContains(t, result, "line 1")
		assert.NotContains(t, result, "line 7")
	})
}

func TestOOMSafety(t *testing.T) {
	m := &azureDevOpsManager{}
	
	t.Run("ClampTailLines", func(t *testing.T) {
		content := "line 1\nline 2"
		result, err := m.streamTail(strings.NewReader(content), 1000000)
		assert.NoError(t, err)
		assert.Equal(t, "line 1\nline 2", result)
	})

	t.Run("ClampFilterContextLines", func(t *testing.T) {
		content := "match"
		result, err := m.streamRegexFilter(strings.NewReader(content), "match", 1000000)
		assert.NoError(t, err)
		assert.Equal(t, "match", result)
	})
}

type logErrorReader struct{}

func (e *logErrorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestLogIOError(t *testing.T) {
	m := &azureDevOpsManager{}
	errReader := &logErrorReader{}

	t.Run("streamTail", func(t *testing.T) {
		result, err := m.streamTail(errReader, 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
		assert.Empty(t, result)
	})

	t.Run("streamHead", func(t *testing.T) {
		result, err := m.streamHead(errReader, 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
		assert.Empty(t, result)
	})

	t.Run("streamPagination", func(t *testing.T) {
		result, err := m.streamPagination(errReader, 1, 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
		assert.Empty(t, result)
	})

	t.Run("streamRegexFilter", func(t *testing.T) {
		result, err := m.streamRegexFilter(errReader, "match", 5)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "log stream interrupted")
		assert.Empty(t, result)
	})
}

func TestProcessLogContent_BufferTooLong(t *testing.T) {
	m := &azureDevOpsManager{}

	// Create a single line of text that exceeds the 1MB bufio.Scanner maxCapacity limit
	massiveLine := strings.Repeat("A", (1*1024*1024)+100)

	result, err := m.processLogContent(strings.NewReader(massiveLine), 0, 0, "", 0, 0, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "log stream interrupted")
	assert.Contains(t, err.Error(), "token too long")
	assert.Empty(t, result)
}
