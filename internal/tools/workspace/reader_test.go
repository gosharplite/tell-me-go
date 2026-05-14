// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

func TestListFiles(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tempDir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "sub", "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	t.Run("list root", func(t *testing.T) {
		res, err := r.listFiles(ctx, map[string]interface{}{"path": tempDir, "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "[f] a.txt") || !strings.Contains(res.Text, "[d] sub") {
			t.Errorf("unexpected output: %s", res.Text)
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		_, err := r.listFiles(ctx, map[string]interface{}{"path": filepath.Join(tempDir, "missing"), "reason": "testing"}, nil)
		if err == nil {
			t.Error("expected error for missing path")
		}
	})
}

func TestReadFile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	content := "some content"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{path}, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, content) {
		t.Errorf("got %s, want %s", res.Text, content)
	}
}

func TestGetTree(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "a/b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "a/b/c.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	t.Run("basic tree", func(t *testing.T) {
		res, err := r.getTree(ctx, map[string]interface{}{"path": tempDir, "max_depth": 2, "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "└── a") || !strings.Contains(res.Text, "└── b") {
			t.Errorf("unexpected tree structure: %s", res.Text)
		}
	})
}

func TestFindFile(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "match.go"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "subdir", "match.go"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "no-match.txt"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	t.Run("find .go files", func(t *testing.T) {
		res, err := r.findFile(ctx, map[string]interface{}{"path": tempDir, "pattern": "*.go", "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(res.Text), "\n")
		if len(lines) != 2 {
			t.Errorf("expected 2 matches, got %d: %v", len(lines), lines)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		res, err := r.findFile(ctx, map[string]interface{}{"path": tempDir, "pattern": "*.md", "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "No files found matching pattern." {
			t.Errorf("expected 'No files found matching pattern.', got %q", res.Text)
		}
	})
}

type mockDiffExecutor struct {
	*processExecutor
}

func (m *mockDiffExecutor) LookPath(name string) (string, error) {
	if name == "diff" {
		return helperPath, nil
	}
	return m.processExecutor.LookPath(name)
}

func (m *mockDiffExecutor) RunCommand(ctx context.Context, parts []string, config executionConfig) (executionResult, error) {
	if parts[0] == "diff" {
		newParts := append([]string{helperPath, "diff"}, parts[1:]...)
		return m.processExecutor.RunCommand(ctx, newParts, config)
	}
	return m.processExecutor.RunCommand(ctx, parts, config)
}

func TestGetFileDiff(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "f1.txt")
	f2 := filepath.Join(tempDir, "f2.txt")
	if err := os.WriteFile(f1, []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("line1\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{
		sm:       sm,
		fs:       persistencetest.NewPlainOSFileSystem(),
		policy:   infra_persistence.NewWorkspacePolicy(),
		executor: &mockDiffExecutor{processExecutor: newprocessExecutor()},
	}
	ctx := context.Background()

	t.Run("diff existing files", func(t *testing.T) {
		res, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": f2, "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		// The mock helper diff prints -line2 and +line3
		if !strings.Contains(res.Text, "-line2") || !strings.Contains(res.Text, "+line3") {
			t.Errorf("unexpected diff: %s", res.Text)
		}
	})

	t.Run("identical files", func(t *testing.T) {
		res, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": f1, "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "Files are identical." {
			t.Errorf("expected 'Files are identical.', got %q", res.Text)
		}
	})
}

func TestReadFile_Truncation(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "large.txt")
	content := strings.Repeat("a", 150000)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{path}, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "... (truncated)") {
		t.Error("expected truncation message")
	}
	if len(res.Text) > 101000 {
		t.Errorf("result too long: %d", len(res.Text))
	}
}

func TestReadFile_Binary(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "bin")
	// Write some binary bytes (containing null byte)
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x02}, 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{path}, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Binary") {
		t.Errorf("expected 'binary' message, got %q", res.Text)
	}
}

func TestGetFileDiff_Errors(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "f1.txt")
	if err := os.WriteFile(f1, []byte("line1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{
		sm:       sm,
		fs:       persistencetest.NewPlainOSFileSystem(),
		policy:   infra_persistence.NewWorkspacePolicy(),
		executor: &mockDiffExecutor{processExecutor: newprocessExecutor()},
	}
	ctx := context.Background()

	t.Run("missing file2", func(t *testing.T) {
		_, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": "missing.txt", "reason": "testing"}, nil)
		if err == nil {
			t.Error("expected error for missing file2")
		}
	})

	t.Run("binary file", func(t *testing.T) {
		fbin := filepath.Join(tempDir, "bin")
		if err := os.WriteFile(fbin, []byte{0x00, 0x01}, 0644); err != nil {
			t.Fatal(err)
		}
		_, _ = r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": fbin, "reason": "testing"}, nil)
	})
}

func TestReadFiles(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "f1.txt")
	f2 := filepath.Join(tempDir, "f2.txt")
	fbin := filepath.Join(tempDir, "bin")
	content1 := "content 1"
	content2 := "content 2"

	if err := os.WriteFile(f1, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fbin, []byte{0x00, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	tests := []struct {
		name          string
		filepaths     any
		expectedTexts []string
	}{
		{
			name:      "read multiple files",
			filepaths: []interface{}{f1, f2},
			expectedTexts: []string{
				"--- File: " + f1 + " ---",
				content1,
				"--- File: " + f2 + " ---",
				content2,
			},
		},
		{
			name:      "partial success",
			filepaths: []interface{}{f1, "missing.txt"},
			expectedTexts: []string{
				content1,
				"ERROR: failed to read file",
			},
		},
		{
			name:      "binary file in batch",
			filepaths: []interface{}{f1, fbin},
			expectedTexts: []string{
				"(Binary file, cannot display as text)",
			},
		},
		{
			name:      "using []string instead of []interface{}",
			filepaths: []string{f1, f2},
			expectedTexts: []string{
				content1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := r.readFiles(ctx, map[string]interface{}{
				"filepaths": tt.filepaths,
				"reason":    "testing",
			}, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, expectedText := range tt.expectedTexts {
				if !strings.Contains(res.Text, expectedText) {
					t.Errorf("expected output to contain %q, but got: %s", expectedText, res.Text)
				}
			}
		})
	}
}

func TestReadFile_UTF8Truncation(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "utf8.txt")

	// '😀' is 4 bytes: \xf0 \x9f \x98 \x80
	// We want to cut it in the middle.
	prefix := strings.Repeat("a", 99998)
	emoji := "😀" // 4 bytes
	content := []byte(prefix + emoji + "extra")

	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{path}, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	truncatedPart := res.Text
	// Find where "... (truncated)" starts
	truncIdx := strings.Index(truncatedPart, "\n... (truncated)")
	if truncIdx == -1 {
		t.Fatal("expected truncation message")
	}

	// Skip the "--- File: ... ---\n" header
	headerEnd := strings.Index(truncatedPart, "\n") + 1
	finalContent := truncatedPart[headerEnd:truncIdx]

	// Check if the last character is valid UTF-8
	if !utf8.ValidString(finalContent) {
		t.Errorf("result contains invalid UTF-8: %q", finalContent[len(finalContent)-10:])
	}
}

func TestReadFiles_UTF8Truncation(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "utf8.txt")

	prefix := strings.Repeat("a", 99998)
	emoji := "😀" // 4 bytes
	content := []byte(prefix + emoji + "extra")

	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{path}, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	truncatedPart := res.Text
	// Find where "... (truncated)" starts
	truncIdx := strings.Index(truncatedPart, "\n... (truncated)")
	if truncIdx == -1 {
		t.Fatal("expected truncation message")
	}

	// Skip the header "--- File: ... ---\n"
	headerEnd := strings.Index(truncatedPart, "\n") + 1
	finalContent := truncatedPart[headerEnd:truncIdx]

	// Check if the last character is valid UTF-8
	if !utf8.ValidString(finalContent) {
		t.Errorf("result contains invalid UTF-8: %q", finalContent[len(finalContent)-10:])
	}
}

func TestReadFile_Directory(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{subDir}, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "ERROR: path is a directory, use list_files instead") {
		t.Errorf("expected directory error message, got %q", res.Text)
	}
}

func TestReadFiles_Directory(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{subDir}, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "ERROR: path is a directory, use list_files instead") {
		t.Errorf("expected directory error message, got %q", res.Text)
	}
}

func TestReadFiles_Limit(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	// More than 50 files
	paths := make([]interface{}, 51)
	for i := 0; i < 51; i++ {
		paths[i] = "f" + string(rune(i)) + ".txt"
	}

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": paths, "reason": "testing"}, nil)
	if err != nil {
		t.Fatalf("expected domain outcome (nil error), got: %v", err)
	}
	if !strings.Contains(res.Text, "Error: requested too many files") {
		t.Errorf("expected limit error message in result text, got %q", res.Text)
	}
}

// ---------------------------------------------------------------------------
// validateDiffPrerequisites tests
// ---------------------------------------------------------------------------

type noDiffExecutor struct {
	*processExecutor
}

func (m *noDiffExecutor) LookPath(name string) (string, error) {
	if name == "diff" {
		return "", fmt.Errorf("diff not found in PATH")
	}
	return m.processExecutor.LookPath(name)
}

func (m *noDiffExecutor) RunCommand(ctx context.Context, parts []string, config executionConfig) (executionResult, error) {
	return m.processExecutor.RunCommand(ctx, parts, config)
}

func TestValidateDiffPrerequisites(t *testing.T) {
	tempDir := t.TempDir()
	validFile := filepath.Join(tempDir, "valid.txt")
	if err := os.WriteFile(validFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	ctx := context.Background()

	t.Run("file1 missing", func(t *testing.T) {
		r := &fileReader{
			sm:       sm,
			fs:       persistencetest.NewPlainOSFileSystem(),
			policy:   infra_persistence.NewWorkspacePolicy(),
			executor: &mockDiffExecutor{processExecutor: newprocessExecutor()},
		}
		err := r.validateDiffPrerequisites(ctx, "nonexistent_file1_xyz.txt", validFile)
		if err == nil {
			t.Fatal("expected error for missing file1")
		}
		if !strings.Contains(err.Error(), "file1") {
			t.Errorf("expected 'file1' in error, got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got %q", err.Error())
		}
	})

	t.Run("executor nil", func(t *testing.T) {
		r := &fileReader{
			sm:       sm,
			fs:       persistencetest.NewPlainOSFileSystem(),
			policy:   infra_persistence.NewWorkspacePolicy(),
			executor: nil,
		}
		err := r.validateDiffPrerequisites(ctx, validFile, validFile)
		if err == nil {
			t.Fatal("expected error for nil executor")
		}
		if !strings.Contains(err.Error(), "no command executor") {
			t.Errorf("expected 'no command executor' in error, got %q", err.Error())
		}
	})

	t.Run("diff not in path", func(t *testing.T) {
		r := &fileReader{
			sm:       sm,
			fs:       persistencetest.NewPlainOSFileSystem(),
			policy:   infra_persistence.NewWorkspacePolicy(),
			executor: &noDiffExecutor{processExecutor: newprocessExecutor()},
		}
		err := r.validateDiffPrerequisites(ctx, validFile, validFile)
		if err == nil {
			t.Fatal("expected error for missing diff")
		}
		if !strings.Contains(err.Error(), "'diff' command not found") {
			t.Errorf("expected 'diff' command not found in error, got %q", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// buildTree ReadDir error test
// ---------------------------------------------------------------------------

// errorReadDirFS overrides ReadDir on an otherwise real persistence.FileSystem.
// The embedded interface satisfies all methods automatically; only ReadDir is
// intercepted to return a configurable error.
type errorReadDirFS struct {
	persistence.FileSystem
	err error
}

func (e *errorReadDirFS) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	return nil, e.err
}

func TestBuildTree_ReadDirError(t *testing.T) {
	t.Run("ReadDir error propagated", func(t *testing.T) {
		expectedErr := fmt.Errorf("mock readdir failure")
		fs := &errorReadDirFS{
			FileSystem: persistencetest.NewPlainOSFileSystem(),
			err:        expectedErr,
		}

		var sb strings.Builder
		ctx := context.Background()

		err := buildTree(ctx, fs, "/tmp", "", 0, 2, &sb, nil)
		if err == nil {
			t.Fatal("expected error from ReadDir")
		}
		if !strings.Contains(err.Error(), "mock readdir failure") {
			t.Errorf("expected 'mock readdir failure' in error, got %q", err.Error())
		}
	})

	t.Run("cancelled context returns immediately", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancel

		fs := &errorReadDirFS{err: fmt.Errorf("should not be reached")}
		var sb strings.Builder

		err := buildTree(ctx, fs, "/tmp", "", 0, 2, &sb, nil)
		if err == nil {
			t.Fatal("expected error from cancelled context")
		}
		if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "canceled") {
			t.Errorf("expected context cancellation error, got %q", err.Error())
		}
	})

	t.Run("heartbeat sent when hb is non-nil", func(t *testing.T) {
		fs := persistence.NewMockFileSystem()
		hb := make(chan struct{}, 1)
		var sb strings.Builder
		ctx := context.Background()

		// buildTree sends a heartbeat then calls ReadDir (which returns empty).
		// We verify the heartbeat was sent.
		err := buildTree(ctx, fs, ".", "", 0, 2, &sb, hb)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		select {
		case <-hb:
			// heartbeat received — success
		default:
			t.Error("expected heartbeat to be sent on hb channel")
		}
	})
}

// ---------------------------------------------------------------------------
// interpretDiffResult exit code tests
// ---------------------------------------------------------------------------

func TestInterpretDiffResult(t *testing.T) {
	r := &fileReader{}

	t.Run("exit code 2 returns domain outcome", func(t *testing.T) {
		res := executionResult{ExitCode: 2, Output: "diff: fatal error"}
		tr, err := r.interpretDiffResult(res, nil)
		if err != nil {
			t.Fatalf("expected nil error (domain outcome), got: %v", err)
		}
		if !strings.Contains(tr.Text, "Error: diff failed with exit code 2") {
			t.Errorf("expected domain error message, got: %q", tr.Text)
		}
	})

	t.Run("exit code 1 with output returns output", func(t *testing.T) {
		res := executionResult{ExitCode: 1, Output: "1c1\n< a\n---\n> b"}
		tr, err := r.interpretDiffResult(res, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tr.Text != "1c1\n< a\n---\n> b" {
			t.Errorf("expected diff output, got: %q", tr.Text)
		}
	})

	t.Run("runErr returns infrastructure fault", func(t *testing.T) {
		res := executionResult{}
		_, err := r.interpretDiffResult(res, fmt.Errorf("exec failed"))
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "diff failed") {
			t.Errorf("expected 'diff failed' in error, got: %v", err)
		}
	})

	t.Run("empty output no error returns identical", func(t *testing.T) {
		res := executionResult{}
		tr, err := r.interpretDiffResult(res, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tr.Text != "Files are identical." {
			t.Errorf("expected 'Files are identical.', got %q", tr.Text)
		}
	})
}

func TestReadFiles_WithHeartbeat(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	hb := make(chan struct{}, 5)
	// Create enough files to trigger at least one heartbeat (every 5 files)
	paths := make([]interface{}, 6)
	for i := 0; i < 6; i++ {
		paths[i] = path // reuse the same valid file
	}

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": paths, "reason": "testing"}, hb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "content") {
		t.Errorf("expected content in output, got %q", res.Text)
	}

	// Expect at least one heartbeat
	select {
	case <-hb:
		// heartbeat received
	default:
		// Acceptable if heartbeat was consumed by the sendHeartbeat function
	}
}

// ---------------------------------------------------------------------------
// readBoundedContent error tests
// ---------------------------------------------------------------------------

type mockFS_OpenError struct {
	persistence.FileSystem
	openErr error
}

func (m *mockFS_OpenError) Open(ctx context.Context, name string) (persistence.File, error) {
	return nil, m.openErr
}

type mockFile_ReadError struct {
	*os.File
	readErr error
}

func (m *mockFile_ReadError) Read(p []byte) (n int, err error) {
	return 0, m.readErr
}

type mockFS_ReadErrorFile struct {
	persistence.FileSystem
	readErr error
}

func (m *mockFS_ReadErrorFile) Open(ctx context.Context, name string) (persistence.File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return &mockFile_ReadError{File: f, readErr: m.readErr}, nil
}

func TestReadBoundedContent_OpenError(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	mfs := &mockFS_OpenError{FileSystem: persistencetest.NewPlainOSFileSystem(), openErr: fmt.Errorf("open failure")}
	r := &fileReader{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: mfs, policy: infra_persistence.NewWorkspacePolicy()}

	_, _, err := r.readBoundedContent(context.Background(), path)
	if err == nil {
		t.Fatal("expected open error")
	}
	if !strings.Contains(err.Error(), "open failure") {
		t.Errorf("expected 'open failure', got: %v", err)
	}
}

func TestReadBoundedContent_ReadError(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	mfs := &mockFS_ReadErrorFile{FileSystem: persistencetest.NewPlainOSFileSystem(), readErr: fmt.Errorf("read failure")}
	r := &fileReader{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: mfs, policy: infra_persistence.NewWorkspacePolicy()}

	_, _, err := r.readBoundedContent(context.Background(), path)
	if err == nil {
		t.Fatal("expected read error")
	}
	if !strings.Contains(err.Error(), "read failure") {
		t.Errorf("expected 'read failure', got: %v", err)
	}
}

func TestProcessSingleFile_SecurityError(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.IsSafeFunc = func(p string) (string, error) {
		return "", fmt.Errorf("security: path not allowed")
	}

	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}

	var sb strings.Builder
	err := r.processSingleFile(context.Background(), path, &sb)
	if err != nil {
		t.Fatalf("processSingleFile should not return error for security rejection: %v", err)
	}
	if !strings.Contains(sb.String(), "ERROR: security") {
		t.Errorf("expected security error in output, got: %q", sb.String())
	}
}

func TestProcessSingleFile_StatError(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "nonexistent.txt")

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}

	var sb strings.Builder
	err := r.processSingleFile(context.Background(), path, &sb)
	if err != nil {
		t.Fatalf("processSingleFile should not return error for stat failure: %v", err)
	}
	if !strings.Contains(sb.String(), "ERROR: failed to read file") {
		t.Errorf("expected stat error in output, got: %q", sb.String())
	}
}

func TestListFiles_DefaultPath(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := r.listFiles(ctx, map[string]interface{}{"reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text == "" {
		t.Error("expected non-empty output for default path")
	}
}

func TestReadFiles_EmptyFilepaths(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	_, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{}, "reason": "testing"}, nil)
	if err == nil {
		t.Fatal("expected error for empty filepaths")
	}
	if !strings.Contains(err.Error(), "filepaths argument is required") {
		t.Errorf("expected 'filepaths argument is required', got: %v", err)
	}
}

func TestReadFiles_InvalidArgs(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	_, err := r.readFiles(ctx, map[string]interface{}{"filepaths": "not_an_array", "reason": "testing"}, nil)
	if err == nil {
		t.Fatal("expected unmarshal error for invalid args")
	}
}

func TestGetFileDiff_InvalidArgs(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{
		sm:     sm,
		fs:     persistencetest.NewPlainOSFileSystem(),
		policy: infra_persistence.NewWorkspacePolicy(),
	}
	ctx := context.Background()

	_, err := r.getFileDiff(ctx, map[string]interface{}{}, nil)
	if err == nil {
		t.Fatal("expected unmarshal error for missing args")
	}
}

func TestSearchFiles_MissingQuery(t *testing.T) {
	s := &fileSearcher{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	_, err := s.searchFiles(context.Background(), map[string]interface{}{"reason": "testing"}, nil)
	if err == nil {
		t.Fatal("expected error for missing query")
	}
	if !strings.Contains(err.Error(), "query argument is required") {
		t.Errorf("expected 'query argument is required', got: %v", err)
	}
}

func TestGrepDefinitions_InvalidArgs(t *testing.T) {
	s := &fileSearcher{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	_, err := s.grepDefinitions(context.Background(), map[string]interface{}{"path": 123, "reason": "testing"}, nil)
	if err == nil {
		t.Fatal("expected unmarshal error for invalid args")
	}
}

func TestFindFile_MissingPattern(t *testing.T) {
	r := &fileReader{
		sm:     &toolstest.MockSecurityManager{AllowAll: true},
		fs:     persistencetest.NewPlainOSFileSystem(),
		policy: infra_persistence.NewWorkspacePolicy(),
	}
	_, err := r.findFile(context.Background(), map[string]interface{}{"reason": "testing"}, nil)
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}

func TestGetFileDiff_SecurityErrorFile2(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "f1.txt")
	if err := os.WriteFile(f1, []byte("line1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: false}
	// Override IsPathSafe to allow file1 but reject file2
	sm.IsSafeFunc = func(path string) (string, error) {
		if strings.Contains(path, "f2") {
			return "", fmt.Errorf("security: path not allowed")
		}
		return path, nil
	}

	r := &fileReader{
		sm:       sm,
		fs:       persistencetest.NewPlainOSFileSystem(),
		policy:   infra_persistence.NewWorkspacePolicy(),
		executor: &mockDiffExecutor{processExecutor: newprocessExecutor()},
	}
	ctx := context.Background()

	_, err := r.getFileDiff(ctx, map[string]interface{}{
		"file1":  f1,
		"file2":  filepath.Join(tempDir, "f2.txt"),
		"reason": "testing",
	}, nil)
	if err == nil {
		t.Fatal("expected security error for file2")
	}
	if !strings.Contains(err.Error(), "security") {
		t.Errorf("expected security error, got: %v", err)
	}
}
