// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
)

func TestAuditor_Close(t *testing.T) {
	t.Run("nil auditor (no file)", func(t *testing.T) {
		a := newAuditor(nil)
		if err := a.Close(); err != nil {
			t.Errorf("Close() on nil-file auditor: %v", err)
		}
	})

	t.Run("normal close", func(t *testing.T) {
		a := newAuditor(nil)
		tmp := filepath.Join(t.TempDir(), "audit.log")
		a.SetLogFile(tmp)
		if err := a.Close(); err != nil {
			t.Errorf("Close() error: %v", err)
		}
	})

	t.Run("double close (idempotent)", func(t *testing.T) {
		a := newAuditor(nil)
		tmp := filepath.Join(t.TempDir(), "audit.log")
		a.SetLogFile(tmp)
		if err := a.Close(); err != nil {
			t.Errorf("first Close() error: %v", err)
		}
		if err := a.Close(); err != nil {
			t.Errorf("second Close() error: %v", err)
		}
	})

	t.Run("close after SetLogFile with empty path", func(t *testing.T) {
		a := newAuditor(nil)
		a.SetLogFile("")
		if err := a.Close(); err != nil {
			t.Errorf("Close() after empty SetLogFile: %v", err)
		}
	})

	t.Run("write error during close (file already closed)", func(t *testing.T) {
		a := newAuditor(nil)
		tmp := filepath.Join(t.TempDir(), "audit.log")
		a.SetLogFile(tmp)

		// Close the underlying file manually to force auditor.Close to hit an error
		if err := a.file.Close(); err != nil {
			t.Fatalf("failed to pre-close file: %v", err)
		}

		err := a.Close()
		if err == nil {
			t.Error("expected error from Close() when file already closed")
		}
	})
}

// newTestAuditor is a shorthand for newAuditor(nil) used by tests.
func newTestAuditor() *auditor {
	return newAuditor(nil)
}

func TestAuditor_SetLogFile_EmptyPath(t *testing.T) {
	t.Parallel()
	a := newTestAuditor()
	a.SetLogFile("")
	if a.logger != nil {
		t.Error("expected nil logger after SetLogFile(\"\")")
	}
	if a.file != nil {
		t.Error("expected nil file after SetLogFile(\"\")")
	}
}

func TestAuditor_SetLogFile_ValidPathCreatesFile(t *testing.T) {
	t.Parallel()
	a := newTestAuditor()
	tmp := filepath.Join(t.TempDir(), "audit.log")
	a.SetLogFile(tmp)

	if a.logger == nil {
		t.Error("expected non-nil logger after SetLogFile with valid path")
	}
	if a.file == nil {
		t.Error("expected non-nil file after SetLogFile with valid path")
	}

	// Verify file exists on disk
	if _, err := os.Stat(tmp); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist on disk", tmp)
	}

	// Cleanup
	_ = a.Close()
}

func TestAuditor_SetLogFile_OverwriteExistingFile(t *testing.T) {
	t.Parallel()
	a := newTestAuditor()
	dir := t.TempDir()
	pathA := filepath.Join(dir, "audit-a.log")
	pathB := filepath.Join(dir, "audit-b.log")

	a.SetLogFile(pathA)
	if a.file == nil {
		t.Fatal("expected non-nil file after first SetLogFile")
	}

	a.SetLogFile(pathB) // This should close pathA and open pathB
	if a.file == nil {
		t.Fatal("expected non-nil file after second SetLogFile")
	}

	// Verify new file exists on disk
	if _, err := os.Stat(pathB); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist on disk", pathB)
	}

	_ = a.Close()
}

func TestAuditor_SetLogFile_InvalidPathWarns(t *testing.T) {
	t.Parallel()
	mock := &mockInteractor{}
	a := newAuditor(func() domain.UserInteractor { return mock })
	a.SetLogFile("/nonexistent_dir_xyz_should_not_exist/audit.log")

	if a.logger != nil {
		t.Error("expected nil logger after failed open")
	}
	if len(mock.Warns) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(mock.Warns), mock.Warns)
	}
	if len(mock.Warns) >= 1 && !strings.Contains(mock.Warns[0], "Failed to open command log file") {
		t.Errorf("warning does not contain expected message: %q", mock.Warns[0])
	}
}

func TestAuditor_SetLogFile_InvalidPathNilInteractor(t *testing.T) {
	t.Parallel()
	a := newAuditor(nil) // interactor provider returns nil
	// Must not panic when interactor() returns nil
	a.SetLogFile("/nonexistent_dir_xyz_should_not_exist/audit.log")
	if a.logger != nil {
		t.Error("expected nil logger after failed open")
	}
}

func TestAuditor_LogAudit(t *testing.T) {
	t.Run("no-op with nil logger", func(t *testing.T) {
		a := newAuditor(nil)
		// Must not panic
		a.LogAudit("test", "key", "val")
	})

	t.Run("writes to file", func(t *testing.T) {
		a := newAuditor(nil)
		tmp := filepath.Join(t.TempDir(), "audit.log")
		a.SetLogFile(tmp)

		a.LogAudit("test", "key", "val")
		if err := a.Close(); err != nil {
			t.Fatalf("Close() error: %v", err)
		}

		data, err := os.ReadFile(tmp)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "AUDIT: test") {
			t.Errorf("expected log to contain 'AUDIT: test', got: %s", content)
		}
		if !strings.Contains(content, "key=val") {
			t.Errorf("expected log to contain 'key=val', got: %s", content)
		}
	})
}
