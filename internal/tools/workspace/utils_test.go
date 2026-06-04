// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

func TestFormatMatch(t *testing.T) {
	path := "test.txt"
	lineNum := 10
	text := "  some text  "

	got := formatMatch(path, lineNum, text)
	expected := "test.txt:10: some text"
	if got != expected {
		t.Errorf("formatMatch() = %q, want %q", got, expected)
	}

	// Test truncation
	longText := strings.Repeat("a", 600)
	got = formatMatch(path, lineNum, longText)
	if !strings.Contains(got, "(truncated)") {
		t.Error("expected truncation for long line")
	}
	if len(got) > 550 { // approximate
		t.Errorf("formatted match too long: %d", len(got))
	}
}

func TestWalkAndProcess(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "safe"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "safe/f1.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.RegisterSafePath(tempDir)
	sm.IsSafeFunc = func(path string) (string, error) {
		if strings.HasPrefix(path, tempDir) {
			return path, nil
		}
		return "", os.ErrPermission
	}

	ctx := context.Background()
	var seen []string
	processor := func(path string) error {
		seen = append(seen, filepath.Base(path))
		return nil
	}

	t.Run("safe path", func(t *testing.T) {
		err := walkAndProcess(ctx, sm, persistencetest.NewPlainOSFileSystem(), tempDir, nil, processor, infra_persistence.NewWorkspacePolicy())
		if err != nil {
			t.Fatal(err)
		}
		if len(seen) != 1 || seen[0] != "f1.txt" {
			t.Errorf("unexpected files seen: %v", seen)
		}
	})

	t.Run("unsafe path", func(t *testing.T) {
		err := walkAndProcess(ctx, sm, persistencetest.NewPlainOSFileSystem(), "/etc", nil, processor, infra_persistence.NewWorkspacePolicy())
		if err == nil {
			t.Error("expected error for unsafe path")
		}
	})
}

func TestSendHeartbeat_DefaultCase(t *testing.T) {
	// Create an unbuffered channel with no reader — the send will block and fall to default
	hb := make(chan struct{})
	ctx := context.Background()

	// This should not panic; the default case prevents blocking
	sendHeartbeat(ctx, hb)

	// Verify no panic occurred (implicit — reaching here is success)
}

func TestSendHeartbeat_NilChannel(t *testing.T) {
	ctx := context.Background()
	// Nil channel — should return immediately
	sendHeartbeat(ctx, nil)
	// No panic == success
}

// mockCheckBinaryFile implements persistence.File for testing checkBinary.
type mockCheckBinaryFile struct {
	readErr error
	seekErr error
	data    []byte
}

func (m *mockCheckBinaryFile) Read(p []byte) (n int, err error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	return copy(p, m.data), io.EOF
}

func (m *mockCheckBinaryFile) Seek(offset int64, whence int) (int64, error) {
	if m.seekErr != nil {
		return 0, m.seekErr
	}
	return 0, nil
}

func (m *mockCheckBinaryFile) Close() error                            { return nil }
func (m *mockCheckBinaryFile) Write(p []byte) (int, error)             { return 0, nil }
func (m *mockCheckBinaryFile) ReadAt(p []byte, off int64) (int, error) { return 0, nil }
func (m *mockCheckBinaryFile) ReadDir(n int) ([]os.DirEntry, error)    { return nil, nil }
func (m *mockCheckBinaryFile) Sync() error                             { return nil }

func TestCheckBinary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		data       []byte
		readErr    error
		seekErr    error
		wantBinary bool
		wantErr    string
	}{
		{
			name:       "text file returns false",
			data:       []byte("hello world"),
			wantBinary: false,
		},
		{
			name:       "binary file returns true",
			data:       bytes.Repeat([]byte{0x00}, 512),
			wantBinary: true,
		},
		{
			name:    "read error (non-EOF) propagated",
			readErr: fmt.Errorf("disk failure"),
			wantErr: "disk failure",
		},
		{
			name:       "seek error propagated",
			data:       []byte("hello"),
			seekErr:    fmt.Errorf("seek failed"),
			wantBinary: false,
			wantErr:    "seek failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockCheckBinaryFile{
				data:    tt.data,
				readErr: tt.readErr,
				seekErr: tt.seekErr,
			}

			gotBinary, gotErr := checkBinary(mock)

			if gotBinary != tt.wantBinary {
				t.Errorf("checkBinary() binary = %v, want %v", gotBinary, tt.wantBinary)
			}

			if tt.wantErr != "" {
				if gotErr == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(gotErr.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, gotErr.Error())
				}
			} else {
				if gotErr != nil {
					t.Errorf("unexpected error: %v", gotErr)
				}
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestHeartbeatAndSendPath(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		hb           chan<- struct{}
		path         string
		pathsChanCap int
		wantErr      error
	}{
		{
			name:         "nil_hb_sends_path_successfully",
			ctx:          context.Background(),
			hb:           nil,
			path:         "test.go",
			pathsChanCap: 1,
			wantErr:      nil,
		},
		{
			name:         "non-nil_hb_sends_heartbeat_and_path",
			ctx:          context.Background(),
			hb:           make(chan struct{}, 1),
			path:         "test.go",
			pathsChanCap: 1,
			wantErr:      nil,
		},
		{
			name:         "ctx_cancelled_after_heartbeat",
			ctx:          canceledContext(),
			hb:           make(chan struct{}, 1),
			path:         "test.go",
			pathsChanCap: 1,
			wantErr:      context.Canceled,
		},
		{
			name:         "ctx_cancelled_in_select",
			ctx:          canceledContext(),
			hb:           nil,
			path:         "test.go",
			pathsChanCap: 0,
			wantErr:      context.Canceled,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			p := &searchPipeline{
				pathsChan: make(chan string, tt.pathsChanCap),
				hb:        tt.hb,
			}

			// Drain pathsChan for success cases so the send doesn't block
			if tt.wantErr == nil {
				go func() { <-p.pathsChan }()
			}

			err := p.heartbeatAndSendPath(tt.ctx, tt.path)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err != tt.wantErr {
					t.Errorf("expected %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestWalkHeartbeat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		count   int
		hb      chan<- struct{}
		ctx     context.Context
		wantErr error
	}{
		{
			name:    "nil channel skips heartbeat",
			count:   50,
			hb:      nil,
			ctx:     ctx,
			wantErr: nil,
		},
		{
			name:    "non-multiple count skips heartbeat",
			count:   7,
			hb:      make(chan struct{}, 1),
			ctx:     ctx,
			wantErr: nil,
		},
		{
			name:    "heartbeat sent when conditions met",
			count:   50,
			hb:      make(chan struct{}, 1),
			ctx:     ctx,
			wantErr: nil,
		},
		{
			name:    "cancelled context error returned",
			count:   50,
			hb:      make(chan struct{}, 1),
			ctx:     cancelCtx,
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := walkHeartbeat(tt.ctx, tt.count, tt.hb)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr.Error(), err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
