// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// logFilterOptions configures log filtering behavior.
type logFilterOptions struct {
	MaxLines     int
	ContextLines int
}

// filterResult contains the processed log content and metadata.
type filterResult struct {
	Content    string
	Truncated  bool
	TotalLines int
}

// ---------------------------------------------------------------------------
// Core dispatcher
// ---------------------------------------------------------------------------

func processLogContent(ctx context.Context, reader io.Reader, tailLines, headLines int, filterQuery string, contextLines, startLine, maxLines int, hb chan<- struct{}) (filterResult, error) {
	if filterQuery != "" {
		return streamRegexFilter(ctx, reader, filterQuery, logFilterOptions{MaxLines: maxLines, ContextLines: contextLines}, hb)
	}

	if startLine > 0 || maxLines > 0 {
		return streamPagination(ctx, reader, startLine, maxLines, hb)
	}

	if headLines > 0 {
		return streamHead(ctx, reader, headLines, hb)
	}

	if tailLines <= 0 {
		tailLines = 200
	}
	return streamTail(ctx, reader, tailLines, hb)
}

// ---------------------------------------------------------------------------
// Filter subsystem
// ---------------------------------------------------------------------------

func streamRegexFilter(ctx context.Context, reader io.Reader, query string, opts logFilterOptions, hb chan<- struct{}) (filterResult, error) {
	re, err := regexp.Compile(query)
	if err != nil {
		return filterResult{}, fmt.Errorf("invalid filter_query regex: %w", err)
	}

	state := newLogFilterState(opts.ContextLines)
	maxMatchedLines := opts.MaxLines
	if maxMatchedLines <= 0 {
		maxMatchedLines = 1000
	}
	matchCount := 0
	truncated := false

	_, err = scanLog(ctx, reader, hb, func(line string) (bool, error) {
		if re.MatchString(line) {
			if matchCount >= maxMatchedLines {
				truncated = true
				return false, nil
			}
			state.addMatch(line)
			matchCount++
		} else {
			state.addNonMatch(line)
		}
		state.updateWindow(line)
		return true, nil
	})

	if err != nil {
		return filterResult{}, err
	}

	content := state.result.String()
	if state.result.Len() == 0 {
		content = "No matches found for filter_query."
	}

	return filterResult{
		Content:    content,
		Truncated:  truncated,
		TotalLines: matchCount,
	}, nil
}

type logFilterState struct {
	preWindow          []string
	preCount           int
	postLinesRemaining int
	lastPrintedLineNum int
	currentLineNum     int
	contextLines       int
	result             strings.Builder
}

func newLogFilterState(contextLines int) *logFilterState {
	if contextLines < 0 {
		contextLines = 5
	}
	if contextLines > 100 {
		contextLines = 100
	}
	var preWindow []string
	if contextLines > 0 {
		preWindow = make([]string, contextLines)
	}
	return &logFilterState{
		preWindow:          preWindow,
		contextLines:       contextLines,
		lastPrintedLineNum: -1,
	}
}

func (s *logFilterState) addMatch(line string) {
	s.printPreWindow()
	s.printLine(line, s.currentLineNum)
	s.postLinesRemaining = s.contextLines
}

func (s *logFilterState) addNonMatch(line string) {
	if s.postLinesRemaining > 0 {
		s.printLine(line, s.currentLineNum)
		s.postLinesRemaining--
	}
}

func (s *logFilterState) updateWindow(line string) {
	if s.contextLines > 0 {
		s.preWindow[s.preCount%s.contextLines] = line
	}
	s.preCount++
	s.currentLineNum++
}

func (s *logFilterState) printPreWindow() {
	if s.contextLines <= 0 {
		return
	}
	limitPre := s.contextLines
	if s.preCount < s.contextLines {
		limitPre = s.preCount
	}

	startPre := 0
	if s.preCount >= s.contextLines {
		startPre = s.preCount % s.contextLines
	}

	for i := 0; i < limitPre; i++ {
		lineNum := s.currentLineNum - (limitPre - i)
		s.printLine(s.preWindow[(startPre+i)%s.contextLines], lineNum)
	}
}

func (s *logFilterState) printLine(line string, lineNum int) {
	if lineNum <= s.lastPrintedLineNum {
		return
	}
	if s.lastPrintedLineNum != -1 && lineNum > s.lastPrintedLineNum+1 {
		s.result.WriteString("\n...")
	}
	if s.result.Len() > 0 {
		s.result.WriteString("\n")
	}
	s.result.WriteString(line)
	s.lastPrintedLineNum = lineNum
}

// ---------------------------------------------------------------------------
// Stream helpers
// ---------------------------------------------------------------------------

func streamPagination(ctx context.Context, reader io.Reader, startLine, maxLines int, hb chan<- struct{}) (filterResult, error) {
	if startLine <= 0 {
		startLine = 1
	}
	var result strings.Builder
	localCount := 0
	printed := 0
	truncated := false

	totalLines, err := scanLog(ctx, reader, hb, func(line string) (bool, error) {
		localCount++
		if localCount < startLine {
			return true, nil
		}
		if maxLines > 0 && printed >= maxLines {
			truncated = true
			return false, nil
		}
		if printed > 0 {
			result.WriteString("\n")
		}
		result.WriteString(line)
		printed++
		return true, nil
	})

	if err != nil {
		return filterResult{}, err
	}

	content := result.String()
	if totalLines < startLine && totalLines > 0 {
		content = fmt.Sprintf("Start line %d is beyond total lines %d.", startLine, totalLines)
	}

	return filterResult{
		Content:    content,
		Truncated:  truncated,
		TotalLines: printed,
	}, nil
}

func streamHead(ctx context.Context, reader io.Reader, n int, hb chan<- struct{}) (filterResult, error) {
	var result strings.Builder
	localCount := 0
	truncated := false

	count, err := scanLog(ctx, reader, hb, func(line string) (bool, error) {
		localCount++
		if localCount > n {
			truncated = true
			return false, nil
		}
		if localCount > 1 {
			result.WriteString("\n")
		}
		result.WriteString(line)
		return true, nil
	})

	if err != nil {
		return filterResult{}, err
	}

	return filterResult{
		Content:    result.String(),
		Truncated:  truncated,
		TotalLines: count,
	}, nil
}

func streamTail(ctx context.Context, reader io.Reader, n int, hb chan<- struct{}) (filterResult, error) {
	if n <= 0 {
		return filterResult{}, nil
	}
	if n > 10000 {
		n = 10000
	}

	ring := make([]string, n)
	localCount := 0
	count, err := scanLog(ctx, reader, hb, func(line string) (bool, error) {
		ring[localCount%n] = line
		localCount++
		return true, nil
	})

	if err != nil {
		return filterResult{}, err
	}

	if count == 0 {
		return filterResult{}, nil
	}

	content := assembleTail(ring, count, n)

	limit := n
	if count < n {
		limit = count
	}

	return filterResult{
		Content:    content,
		Truncated:  count > n,
		TotalLines: limit,
	}, nil
}

// ---------------------------------------------------------------------------
// Foundation
// ---------------------------------------------------------------------------

// sendScannerHeartbeat sends a heartbeat every 1000 lines scanned.
func sendScannerHeartbeat(hb chan<- struct{}, count int) {
	if count%1000 == 0 && hb != nil {
		select {
		case hb <- struct{}{}:
		default:
		}
	}
}

func scanLog(ctx context.Context, reader io.Reader, hb chan<- struct{}, processFn func(line string) (bool, error)) (int, error) {
	scanner := bufio.NewScanner(reader)
	const maxCapacity = 1 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	count := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return count, err
		}

		count++
		sendScannerHeartbeat(hb, count)

		continueScan, err := processFn(scanner.Text())
		if err != nil {
			return count, err
		}
		if !continueScan {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("log stream interrupted: %w", err)
	}

	return count, nil
}

func assembleTail(ring []string, count, n int) string {
	var result strings.Builder
	start := 0
	if count > n {
		start = count % n
	}

	limit := n
	if count < n {
		limit = count
	}

	for i := 0; i < limit; i++ {
		if i > 0 {
			result.WriteString("\n")
		}
		result.WriteString(ring[(start+i)%n])
	}
	return result.String()
}
