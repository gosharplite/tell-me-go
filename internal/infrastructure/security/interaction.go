// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/ui"
	"golang.org/x/term"
)

// InteractionHandler manages terminal locking and user prompts.
type InteractionHandler struct {
	reader     *bufio.Reader
	readerMu   sync.Mutex
	terminalMu sync.Mutex
	auditor    *Auditor
}

// NewInteractionHandler creates a new InteractionHandler.
func NewInteractionHandler(r io.Reader, auditor *Auditor) *InteractionHandler {
	return &InteractionHandler{
		reader:  bufio.NewReader(r),
		auditor: auditor,
	}
}

// SetReader updates the input reader.
func (h *InteractionHandler) SetReader(r io.Reader) {
	h.readerMu.Lock()
	defer h.readerMu.Unlock()
	h.reader = bufio.NewReader(r)
}

// TerminalLock locks the terminal for exclusive access.
func (h *InteractionHandler) TerminalLock() {
	h.terminalMu.Lock()
}

// TerminalUnlock unlocks the terminal.
func (h *InteractionHandler) TerminalUnlock() {
	h.terminalMu.Unlock()
}

// ConfirmAction prompts the user for confirmation.
func (h *InteractionHandler) ConfirmAction(ctx context.Context, action, target, detail string, bypass bool) (bool, error) {
	h.TerminalLock()
	defer h.TerminalUnlock()

	detailLog := detail
	if len(detailLog) > 500 {
		detailLog = detailLog[:500] + "... (truncated)"
	}

	if bypass {
		fmt.Fprintf(os.Stderr, "%s[Auto-Approved] Action '%s' on '%s' auto-approved (bypass_confirmation enabled).%s\n", ui.ColorGreen, action, target, ui.ColorReset)
		if h.auditor != nil {
			h.auditor.LogAudit("ACTION", action+" on "+target, "DETAIL", detailLog+" (auto-approved via bypass_confirmation)")
		}
		return true, nil
	}

	fmt.Fprintf(os.Stderr, "%s[CONFIRMATION REQUIRED]%s\n", ui.ColorBoldYellow, ui.ColorReset)
	fmt.Fprintf(os.Stderr, "AI is requesting to %s: %s\n", action, target)
	if detail != "" {
		if len(detail) > 1000 {
			detail = detail[:1000] + "\n... (truncated)"
		}
		fmt.Fprintf(os.Stderr, "%s%s%s\n", ui.ColorGray, detail, ui.ColorReset)
	}
	fmt.Fprintf(os.Stderr, "Proceed? (y/N) ")

	char, err := h.readSingleKey(ctx)
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		return false, err
	}
	if char == "y" {
		if h.auditor != nil {
			h.auditor.LogAudit("ACTION", action+" on "+target, "DETAIL", detailLog)
		}
		return true, nil
	}
	return false, nil
}

// ReadSingleKey waits for a single key press.
func (h *InteractionHandler) ReadSingleKey(ctx context.Context) (string, error) {
	return h.readSingleKey(ctx)
}

func (h *InteractionHandler) readSingleKey(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if val := os.Getenv("TELL_ME_MOCK_ANSWER"); val != "" {
		return strings.ToLower(val[:1]), nil
	}

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		state, err := term.MakeRaw(fd)
		if err == nil {
			defer func() { _ = term.Restore(fd, state) }()
		}
	} else if os.Getenv("GO_WANT_HELPER_PROCESS") != "" || strings.HasSuffix(os.Args[0], ".test") {
		// Likely in a test environment, skip terminal check and just read
	} else {
		return "", fmt.Errorf("confirmation required but not running in a terminal. Use --bypass-confirmation to skip if running in a non-interactive environment")
	}

	type result struct {
		b   byte
		err error
	}
	resChan := make(chan result, 1)
	go func() {
		h.readerMu.Lock()
		defer h.readerMu.Unlock()
		b, err := h.reader.ReadByte()
		if err != nil {
			resChan <- result{0, err}
		} else {
			resChan <- result{b, nil}
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resChan:
		if res.err != nil {
			return "", res.err
		}
		if res.b == 3 { // Ctrl+C (ETX)
			return "", context.Canceled
		}
		return strings.ToLower(string(res.b)), nil
	}
}

// ReadLine reads a line of input.
func (h *InteractionHandler) ReadLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	type result struct {
		s   string
		err error
	}
	resChan := make(chan result, 1)
	go func() {
		h.readerMu.Lock()
		defer h.readerMu.Unlock()
		s, err := h.reader.ReadString('\n')
		if err != nil && (err != io.EOF || s == "") {
			resChan <- result{"", err}
		} else {
			resChan <- result{s, nil}
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resChan:
		return res.s, res.err
	}
}
