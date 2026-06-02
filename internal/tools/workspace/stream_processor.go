// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// streamProcessor handles line-by-line processing of command output with
// capture limits, truncation detection, and optional file writing.
type streamProcessor struct {
	stdoutStr     *strings.Builder
	stderrStr     *strings.Builder
	mu            *sync.Mutex
	truncated     *atomic.Bool
	wt            *writeTracker
	totalCaptured *int
	maxCapture    int
	feedback      io.Writer
	file          *os.File
}

func (sp *streamProcessor) processLine(sb *strings.Builder, rawLine []byte, prefix string, feedback io.Writer) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.file != nil {
		lineWithNL := append([]byte(nil), rawLine...)
		lineWithNL = append(lineWithNL, '\n')
		sp.wt.Write(sp.file, lineWithNL)
	}

	content := string(rawLine) + "\n"
	feedbackMsg := ""
	if feedback != nil {
		feedbackMsg = fmt.Sprintf("  %s\n", rawLine)
	}

	if prefix != "" {
		content = fmt.Sprintf("%s %s", prefix, content)
		if feedback != nil {
			feedbackMsg = fmt.Sprintf("  %s %s\n", prefix, rawLine)
		}
	}

	remaining := sp.maxCapture - *sp.totalCaptured
	if remaining > 0 {
		if len(content) > remaining {
			sp.truncated.Store(true)
		}
		content = truncateToValidUTF8(content, remaining)
		sb.WriteString(content)
		*sp.totalCaptured += len(content)
	} else {
		sp.truncated.Store(true)
	}

	if feedback != nil && feedbackMsg != "" {
		_, _ = fmt.Fprint(feedback, feedbackMsg)
	}
}

func (sp *streamProcessor) appendErr(sb *strings.Builder, err error) {
	if err == nil {
		return
	}
	msg := fmt.Sprintf("\n[Warning] Output read error: %v", err)
	if err == bufio.ErrTooLong {
		msg = "\n[Warning] Output line too long for scanner; truncated."
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.feedback != nil {
		_, _ = fmt.Fprintln(sp.feedback, msg)
	}

	remaining := sp.maxCapture - *sp.totalCaptured
	if remaining > 0 {
		fullMsg := msg + "\n"
		if len(fullMsg) > remaining {
			sp.truncated.Store(true)
		}
		content := truncateToValidUTF8(fullMsg, remaining)
		sb.WriteString(content)
		*sp.totalCaptured += len(content)
	} else {
		sp.truncated.Store(true)
	}
}
