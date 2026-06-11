// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestWatchHistoryFileCmd(t *testing.T) {
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), nil, mockModifier)

	// 1. Empty filepath
	mockModifier.GetFilePathFunc = func() string { return "" }
	cmd := m.watchHistoryFileCmd()
	if cmd() != nil {
		t.Error("expected nil msg for empty filepath")
	}

	// 2. Non-existent file
	mockModifier.GetFilePathFunc = func() string { return "non-existent" }
	cmd = m.watchHistoryFileCmd()
	if cmd() != nil {
		t.Error("expected nil msg for non-existent file")
	}

	// 3. Cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	tmpFilePath := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFilePath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	m.ctx = ctx
	mockModifier.GetFilePathFunc = func() string { return tmpFilePath }
	cmd = m.watchHistoryFileCmd()
	if cmd() != nil {
		t.Error("expected nil msg for cancelled context")
	}
}

// ── Watcher error path hardening tests (Issue #383) ──

func TestHandleWatcherEvent(t *testing.T) {
	m := &rootBrowserModel{}

	tests := []struct {
		name    string
		event   fsnotify.Event
		ok      bool
		wantNil bool
	}{
		{
			name:    "channel closed (ok=false)",
			event:   fsnotify.Event{},
			ok:      false,
			wantNil: true,
		},
		{
			name:    "write event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Write},
			ok:      true,
			wantNil: false,
		},
		{
			name:    "create event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Create},
			ok:      true,
			wantNil: false,
		},
		{
			name:    "remove event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Remove},
			ok:      true,
			wantNil: false,
		},
		{
			name:    "rename event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Rename},
			ok:      true,
			wantNil: false,
		},
		{
			name:    "chmod event returns nil (not watched)",
			event:   fsnotify.Event{Op: fsnotify.Chmod},
			ok:      true,
			wantNil: true,
		},
		{
			name:    "combined write+create event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Write | fsnotify.Create},
			ok:      true,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := m.handleWatcherEvent(tt.event, tt.ok)
			if tt.wantNil && msg != nil {
				t.Errorf("expected nil, got %T", msg)
			}
			if !tt.wantNil {
				if _, ok := msg.(fileChangedMsg); !ok {
					t.Errorf("expected fileChangedMsg, got %T", msg)
				}
			}
		})
	}
}

func TestHandleWatcherError(t *testing.T) {
	m := &rootBrowserModel{}

	tests := []struct {
		name string
		err  error
		ok   bool
	}{
		{
			name: "channel closed (ok=false)",
			err:  nil,
			ok:   false,
		},
		{
			name: "watcher error with ok=true",
			err:  errors.New("watcher error"),
			ok:   true,
		},
		{
			name: "nil error with ok=true",
			err:  nil,
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := m.handleWatcherError(tt.err, tt.ok)
			// All subtests here return nil (below 3-error threshold)
			if msg != nil {
				t.Errorf("expected nil, got %T", msg)
			}
		})
	}
}

// ── Watcher error message hardening (Issue #433) ──

func TestWatchHistoryFileCmd_WatcherCreateError(t *testing.T) {
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), nil, mockModifier)
	m.watcherFactory = func() (*fsnotify.Watcher, error) {
		return nil, errors.New("inotify instance limit reached")
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	mockModifier.GetFilePathFunc = func() string { return tmpFile }
	cmd := m.watchHistoryFileCmd()
	msg := cmd()

	werr, ok := msg.(watcherErrorMsg)
	if !ok {
		t.Fatalf("expected watcherErrorMsg, got %T (value: %v)", msg, msg)
	}
	if !strings.Contains(werr.err.Error(), "create watcher") {
		t.Errorf("expected error wrapping 'create watcher', got: %v", werr.err)
	}
}

func TestWatchHistoryFileCmd_WatcherAddError(t *testing.T) {
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), nil, mockModifier)

	// Use a factory that returns a real watcher, then close it so Add fails.
	m.watcherFactory = func() (*fsnotify.Watcher, error) {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, err
		}
		_ = w.Close() // Close immediately so Add fails
		return w, nil
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	mockModifier.GetFilePathFunc = func() string { return tmpFile }
	cmd := m.watchHistoryFileCmd()
	msg := cmd()

	werr, ok := msg.(watcherErrorMsg)
	if !ok {
		t.Fatalf("expected watcherErrorMsg, got %T (value: %v)", msg, msg)
	}
	if !strings.Contains(werr.err.Error(), "add watcher") {
		t.Errorf("expected error wrapping 'add watcher', got: %v", werr.err)
	}
}

func TestProcessWatcherEvents_WatcherErrorsChannel(t *testing.T) {
	// Create a watcher and close it so the Errors channel returns (zero, false).
	// This exercises the case err, ok := <-watcher.Errors where ok=false.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	_ = watcher.Close()

	m := &rootBrowserModel{ctx: context.Background()}
	msg := m.processWatcherEvents(watcher)
	// handleWatcherError(nil, false) returns nil
	if msg != nil {
		t.Errorf("expected nil msg from closed watcher, got %T", msg)
	}
}

func TestWatcherErrorMsg_Handling(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)

	newModel, cmd := m.Update(watcherErrorMsg{err: errors.New("watcher failed")})
	updated := newModel.(*rootBrowserModel)

	if updated.err == nil || updated.err.Error() != "watcher failed" {
		t.Errorf("expected err='watcher failed', got %v", updated.err)
	}
	if cmd != nil {
		t.Error("expected nil cmd from watcherErrorMsg handling to prevent hot loop")
	}

	// Verify View() renders the error
	updated.ready = true
	view := updated.View()
	if !strings.Contains(view, "Error: watcher failed") {
		t.Errorf("expected View() to contain error, got %q", view)
	}
}

func TestProcessWatcherEvents_ContextCancellation(t *testing.T) {
	// Create a real watcher on a temp file so it's valid but we cancel context first.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(tmpFile); err != nil {
		t.Fatal(err)
	}

	// Cancel context before calling processWatcherEvents
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &rootBrowserModel{ctx: ctx}
	msg := m.processWatcherEvents(watcher)
	if msg != nil {
		t.Errorf("expected nil msg for cancelled context, got %T", msg)
	}
}

func TestProcessWatcherEvents_ChannelClose(t *testing.T) {
	// Create a watcher, close it immediately, then test processWatcherEvents.
	// When a watcher is closed, its Events channel is closed, so the receive
	// returns event={}, ok=false.

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}

	if err := watcher.Add(tmpFile); err != nil {
		_ = watcher.Close()
		t.Fatal(err)
	}

	// Close the watcher to close the Events channel
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}

	m := &rootBrowserModel{ctx: context.Background()}
	msg := m.processWatcherEvents(watcher)
	if msg != nil {
		t.Errorf("expected nil msg for closed channel, got %T", msg)
	}
}

// ── Watcher error handling hardening (G9 + G10) ──

func TestWatchHistoryFileCmd_StatPermissionError(t *testing.T) {
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), nil, mockModifier)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "no-perms")

	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmpDir, 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(tmpDir, 0755) }()

	mockModifier.GetFilePathFunc = func() string { return tmpFile }
	cmd := m.watchHistoryFileCmd()
	msg := cmd()

	werr, ok := msg.(watcherErrorMsg)
	if !ok {
		t.Fatalf("expected watcherErrorMsg for permission error, got %T (value: %v)", msg, msg)
	}
	if !strings.Contains(werr.err.Error(), "stat history file") {
		t.Errorf("expected 'stat history file', got: %v", werr.err)
	}
}

func TestHandleWatcherError_ConsecutiveThreshold(t *testing.T) {
	m := &rootBrowserModel{}

	// Error 1 — swallowed
	msg := m.handleWatcherError(errors.New("error 1"), true)
	if msg != nil {
		t.Errorf("expected nil for 1st error, got %T", msg)
	}
	if m.watcherErrorCount != 1 {
		t.Errorf("expected 1, got %d", m.watcherErrorCount)
	}

	// Error 2 — swallowed
	msg = m.handleWatcherError(errors.New("error 2"), true)
	if msg != nil {
		t.Errorf("expected nil for 2nd error, got %T", msg)
	}

	// Error 3 — surfaced
	msg = m.handleWatcherError(errors.New("error 3"), true)
	werr, ok := msg.(watcherErrorMsg)
	if !ok {
		t.Fatalf("expected watcherErrorMsg on 3rd error, got %T", msg)
	}
	if !strings.Contains(werr.err.Error(), "failed after 3 errors") {
		t.Errorf("expected 'failed after 3 errors', got: %v", werr.err)
	}
}

func TestHandleWatcherError_ResetAfterSuccess(t *testing.T) {
	m := &rootBrowserModel{watcherErrorCount: 5}

	msg := m.handleWatcherEvent(fsnotify.Event{Op: fsnotify.Write}, true)
	if _, ok := msg.(fileChangedMsg); !ok {
		t.Errorf("expected fileChangedMsg, got %T", msg)
	}
	if m.watcherErrorCount != 0 {
		t.Errorf("expected reset to 0, got %d", m.watcherErrorCount)
	}
}

func TestHandleWatcherError_ChannelClosedResetsNothing(t *testing.T) {
	m := &rootBrowserModel{watcherErrorCount: 2}

	msg := m.handleWatcherError(nil, false)
	if msg != nil {
		t.Errorf("expected nil for closed channel, got %T", msg)
	}
	if m.watcherErrorCount != 2 {
		t.Errorf("expected unchanged at 2, got %d", m.watcherErrorCount)
	}
}
