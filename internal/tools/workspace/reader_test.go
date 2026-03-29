// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("list root", func(t *testing.T) {
		res, err := r.listFiles(ctx, map[string]interface{}{"path": tempDir}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "[f] a.txt") || !strings.Contains(res.Text, "[d] sub") {
			t.Errorf("unexpected output: %s", res.Text)
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		_, err := r.listFiles(ctx, map[string]interface{}{"path": filepath.Join(tempDir, "missing")}, nil)
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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	res, err := r.readFile(ctx, map[string]interface{}{"filepath": path}, nil)
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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("basic tree", func(t *testing.T) {
		res, err := r.getTree(ctx, map[string]interface{}{"path": tempDir, "max_depth": 2}, nil)
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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("find .go files", func(t *testing.T) {
		res, err := r.findFile(ctx, map[string]interface{}{"path": tempDir, "pattern": "*.go"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(res.Text), "\n")
		if len(lines) != 2 {
			t.Errorf("expected 2 matches, got %d: %v", len(lines), lines)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		res, err := r.findFile(ctx, map[string]interface{}{"path": tempDir, "pattern": "*.md"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "No files found matching pattern." {
			t.Errorf("expected 'No files found matching pattern.', got %q", res.Text)
		}
	})
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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("diff existing files", func(t *testing.T) {
		res, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": f2}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "-line2") || !strings.Contains(res.Text, "+line3") {
			t.Errorf("unexpected diff: %s", res.Text)
		}
	})

	t.Run("identical files", func(t *testing.T) {
		res, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": f1}, nil)
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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	res, err := r.readFile(ctx, map[string]interface{}{"filepath": path}, nil)
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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	res, err := r.readFile(ctx, map[string]interface{}{"filepath": path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "binary") {
		t.Errorf("expected 'binary' message, got %q", res.Text)
	}
}

func TestGetFileDiff_Errors(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "f1.txt")
	if err := os.WriteFile(f1, []byte("line1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("missing file2", func(t *testing.T) {
		_, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": "missing.txt"}, nil)
		if err == nil {
			t.Error("expected error for missing file2")
		}
	})

	t.Run("binary file", func(t *testing.T) {
		fbin := filepath.Join(tempDir, "bin")
		if err := os.WriteFile(fbin, []byte{0x00, 0x01}, 0644); err != nil {
			t.Fatal(err)
		}
		res, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": fbin}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ToLower(res.Text), "binary") {
			t.Errorf("expected binary message, got %q", res.Text)
		}
	})
}

func TestReadFiles(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "f1.txt")
	f2 := filepath.Join(tempDir, "f2.txt")
	content1 := "content 1"
	content2 := "content 2"
	if err := os.WriteFile(f1, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("read multiple files", func(t *testing.T) {
		res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{f1, f2}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "--- File: "+f1+" ---") || !strings.Contains(res.Text, content1) {
			t.Errorf("f1 missing or incorrect: %s", res.Text)
		}
		if !strings.Contains(res.Text, "--- File: "+f2+" ---") || !strings.Contains(res.Text, content2) {
			t.Errorf("f2 missing or incorrect: %s", res.Text)
		}
	})

	t.Run("partial success", func(t *testing.T) {
		res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{f1, "missing.txt"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, content1) {
			t.Errorf("f1 missing: %s", res.Text)
		}
		if !strings.Contains(res.Text, "ERROR: failed to read file") {
			t.Errorf("expected error message for missing file: %s", res.Text)
		}
	})

	t.Run("binary file in batch", func(t *testing.T) {
		fbin := filepath.Join(tempDir, "bin")
		if err := os.WriteFile(fbin, []byte{0x00, 0x01}, 0644); err != nil {
			t.Fatal(err)
		}
		res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{f1, fbin}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "(Binary file, cannot display as text)") {
			t.Errorf("expected binary message for fbin: %s", res.Text)
		}
	})

	t.Run("using []string instead of []interface{}", func(t *testing.T) {
		res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []string{f1, f2}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, content1) {
			t.Errorf("f1 missing: %s", res.Text)
		}
	})
}

func TestReadFile_UTF8Truncation(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "utf8.txt")

	// '😀' is 4 bytes: \xf0 \x9f \x98 \x80
	// We want to cut it in the middle.
	// We'll put it at index 99,998.
	prefix := strings.Repeat("a", 99998)
	emoji := "😀" // 4 bytes
	content := []byte(prefix + emoji + "extra")

	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	res, err := r.readFile(ctx, map[string]interface{}{"filepath": path}, nil)
	if err != nil {
		t.Fatal(err)
	}

	truncatedPart := res.Text
	// Find where "... (truncated)" starts
	truncIdx := strings.Index(truncatedPart, "\n... (truncated)")
	if truncIdx == -1 {
		t.Fatal("expected truncation message")
	}

	finalContent := truncatedPart[:truncIdx]

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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{path}}, nil)
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

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	_, err := r.readFile(ctx, map[string]interface{}{"filepath": subDir}, nil)
	if err == nil {
		t.Error("expected error when reading a directory")
	} else if !strings.Contains(err.Error(), "path is a directory") {
		t.Errorf("expected error message to contain 'path is a directory', got %q", err.Error())
	}
}

func TestReadFiles_Directory(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	res, err := r.readFiles(ctx, map[string]interface{}{"filepaths": []interface{}{subDir}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "ERROR: path is a directory, use list_files instead") {
		t.Errorf("expected directory error message, got %q", res.Text)
	}
}

func TestReadFiles_Limit(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	// More than 50 files
	paths := make([]interface{}, 51)
	for i := 0; i < 51; i++ {
		paths[i] = "f" + string(rune(i)) + ".txt"
	}

	_, err := r.readFiles(ctx, map[string]interface{}{"filepaths": paths}, nil)
	if err == nil {
		t.Fatal("expected error for exceeding file limit")
	}
	if !strings.Contains(err.Error(), "requested too many files") {
		t.Errorf("expected limit error message, got %q", err.Error())
	}
}
