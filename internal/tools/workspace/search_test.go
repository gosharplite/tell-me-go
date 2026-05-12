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

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

func TestSearchFiles_SkipsBinary(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "search_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	binaryPath := filepath.Join(tempDir, "binary.bin")
	err = os.WriteFile(binaryPath, []byte{0, 1, 2, 3}, 0644)
	if err != nil {
		t.Fatal(err)
	}

	textPath := filepath.Join(tempDir, "text.txt")
	err = os.WriteFile(textPath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	s := &fileSearcher{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}

	ctx := context.Background()
	args := map[string]interface{}{
		"path":   tempDir,
		"query":  "hello",
		"reason": "testing",
	}

	result, err := s.searchFiles(ctx, args, nil)
	if err != nil {
		t.Fatalf("searchFiles failed: %v", err)
	}

	if !strings.Contains(result.Text, "text.txt:1: hello world") {
		t.Errorf("expected result to contain text file match, got %q", result.Text)
	}

	if strings.Contains(result.Text, "binary.bin") {
		t.Error("expected result NOT to contain binary file match")
	}
}

func TestGrepDefinitions(t *testing.T) {
	t.Run("Functions", testGrepFunctions)
	t.Run("Structs", testGrepStructs)
	t.Run("Interfaces", testGrepInterfaces)
	t.Run("ComplexPatterns", testGrepComplexPatterns)
	t.Run("ErrorPaths", testGrepErrorPaths)
}

func setupGrepTest(t *testing.T, files map[string]string) (persistence.FileSystem, string) {
	tempDir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(tempDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return persistencetest.NewPlainOSFileSystem(), tempDir
}

type grepResult struct {
	Line string
}

type grepexpectedResult struct {
	Content string
}

func verifyGrepResults(t *testing.T, results []grepResult, expected []grepexpectedResult) {
	t.Helper()
	if len(expected) == 0 {
		verifyNoResults(t, results)
		return
	}
	for _, exp := range expected {
		verifyContains(t, results, exp.Content)
	}
}

func verifyNoResults(t *testing.T, results []grepResult) {
	t.Helper()
	if len(results) != 1 || results[0].Line != "No definitions found." {
		t.Errorf("expected 'No definitions found.', got %+v", results)
	}
}

func verifyContains(t *testing.T, results []grepResult, content string) {
	t.Helper()
	for _, res := range results {
		if strings.Contains(res.Line, content) {
			return
		}
	}
	t.Errorf("expected content %q not found in results", content)
}

func toGrepResults(text string) []grepResult {
	lines := strings.Split(text, "\n")
	results := make([]grepResult, len(lines))
	for i, l := range lines {
		results[i] = grepResult{Line: l}
	}
	return results
}

func testGrepFunctions(t *testing.T) {
	fs, root := setupGrepTest(t, map[string]string{
		"script.py": "def my_func():\n    pass",
		"script.js": "function jsFunc() {}\nconst arrow = () => {}",
		"main.go":   "func main() {}",
	})
	s := &fileSearcher{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: fs, policy: infra_persistence.NewWorkspacePolicy()}
	res, err := s.grepDefinitions(context.Background(), map[string]interface{}{"path": root, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifyGrepResults(t, toGrepResults(res.Text), []grepexpectedResult{
		{Content: "def my_func"},
		{Content: "function jsFunc"},
		{Content: "const arrow"},
		{Content: "func main"},
	})
}

func testGrepStructs(t *testing.T) {
	fs, root := setupGrepTest(t, map[string]string{
		"data.go": "type User struct {\n    ID int\n}",
		"app.py":  "class App:\n    pass",
	})
	s := &fileSearcher{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: fs, policy: infra_persistence.NewWorkspacePolicy()}
	res, err := s.grepDefinitions(context.Background(), map[string]interface{}{"path": root, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifyGrepResults(t, toGrepResults(res.Text), []grepexpectedResult{
		{Content: "type User struct"},
		{Content: "class App"},
	})
}

func testGrepInterfaces(t *testing.T) {
	fs, root := setupGrepTest(t, map[string]string{
		"service.go": "type Service interface {\n    Run()\n}",
	})
	s := &fileSearcher{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: fs, policy: infra_persistence.NewWorkspacePolicy()}
	res, err := s.grepDefinitions(context.Background(), map[string]interface{}{"path": root, "reason": "testing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifyGrepResults(t, toGrepResults(res.Text), []grepexpectedResult{
		{Content: "type Service interface"},
	})
}

func testGrepComplexPatterns(t *testing.T) {
	fs, root := setupGrepTest(t, map[string]string{
		"script.py": "def my_func():\n    pass\nclass MyClass:\n    pass",
	})
	s := &fileSearcher{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: fs, policy: infra_persistence.NewWorkspacePolicy()}

	t.Run("with query", func(t *testing.T) {
		res, err := s.grepDefinitions(context.Background(), map[string]interface{}{"path": root, "query": "my_func", "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		results := toGrepResults(res.Text)
		verifyGrepResults(t, results, []grepexpectedResult{{Content: "def my_func"}})
		for _, r := range results {
			if strings.Contains(r.Line, "class MyClass") {
				t.Error("result should not contain class MyClass when querying for my_func")
			}
		}
	})
}

func testGrepErrorPaths(t *testing.T) {
	fs, root := setupGrepTest(t, map[string]string{})
	s := &fileSearcher{sm: &toolstest.MockSecurityManager{AllowAll: true}, fs: fs, policy: infra_persistence.NewWorkspacePolicy()}

	t.Run("no results", func(t *testing.T) {
		res, err := s.grepDefinitions(context.Background(), map[string]interface{}{"path": root, "query": "nonexistent", "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		verifyGrepResults(t, toGrepResults(res.Text), nil)
	})
}

func TestSearchFiles_TooManyResults(t *testing.T) {
	tempDir := t.TempDir()
	// The limit in searchFiles is hardcoded to 100.
	// We need 101 matches to trigger truncation.
	for i := 0; i < 101; i++ {
		if err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i)), []byte("match"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	s := &fileSearcher{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	args := map[string]interface{}{
		"path":   tempDir,
		"query":  "match",
		"reason": "testing",
	}

	res, err := s.searchFiles(ctx, args, nil)
	if err != nil {
		t.Fatalf("searchFiles failed: %v", err)
	}

	if !strings.Contains(res.Text, "... (truncated)") {
		t.Error("expected truncation message in results")
	}

	lines := strings.Split(strings.TrimSpace(res.Text), "\n")
	// Last line is truncation message, so we expect 101 lines if it includes the message as a line or similar.
	// Actually strings.Join(results, "\n") + "\n... (truncated)"
	if len(lines) < 100 {
		t.Errorf("expected at least 100 results before truncation, got %d", len(lines))
	}
}
