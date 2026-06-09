// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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

// ---------------------------------------------------------------------------
// createSearchMatcher tests
// ---------------------------------------------------------------------------

// TestCreateSearchMatcher_LiteralMatch verifies the literal match
// path returns true when a line contains the query string.
func TestCreateSearchMatcher_LiteralMatch(t *testing.T) {
	s := &fileSearcher{}
	matcher, err := s.createSearchMatcher("hello", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matcher == nil {
		t.Fatal("expected non-nil matcher")
	}
	_, ok := matcher("ignored-path.txt", "hello world")
	if !ok {
		t.Error("expected true for line containing 'hello'")
	}
}

// TestCreateSearchMatcher_LiteralNoMatch verifies the literal match
// path returns false when a line does not contain the query string.
func TestCreateSearchMatcher_LiteralNoMatch(t *testing.T) {
	s := &fileSearcher{}
	matcher, err := s.createSearchMatcher("hello", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, ok := matcher("ignored-path.txt", "goodbye world")
	if ok {
		t.Error("expected false for line not containing 'hello'")
	}
}

// TestCreateSearchMatcher_RegexValidMatch verifies the regex match
// path returns true when a line matches the pattern.
func TestCreateSearchMatcher_RegexValidMatch(t *testing.T) {
	s := &fileSearcher{}
	matcher, err := s.createSearchMatcher("^func", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matcher == nil {
		t.Fatal("expected non-nil matcher")
	}
	_, ok := matcher("ignored.go", "func main() {}")
	if !ok {
		t.Error("expected true for line starting with 'func'")
	}
}

// TestCreateSearchMatcher_RegexValidNoMatch verifies the regex match
// path returns false when a line does not match the pattern.
func TestCreateSearchMatcher_RegexValidNoMatch(t *testing.T) {
	s := &fileSearcher{}
	matcher, err := s.createSearchMatcher("^func", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, ok := matcher("ignored.go", "var x = 1")
	if ok {
		t.Error("expected false for line not starting with 'func'")
	}
}

// TestCreateSearchMatcher_RegexInvalid verifies that an invalid regex
// pattern returns an error and a nil matcher.
func TestCreateSearchMatcher_RegexInvalid(t *testing.T) {
	s := &fileSearcher{}
	matcher, err := s.createSearchMatcher("[invalid", true)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if matcher != nil {
		t.Error("expected nil matcher for invalid regex")
	}
	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("expected 'invalid regex' in error, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// getDefinitionMatcher — regex query edge cases
// ---------------------------------------------------------------------------

func TestGetDefinitionMatcher_RegexQueryFiltersNonMatchingDef(t *testing.T) {
	t.Parallel()

	compiledDefs := getCompiledPatterns()

	tests := []struct {
		name      string
		reQuery   *regexp.Regexp
		path      string
		line      string
		wantMatch string
		wantOK    bool
	}{
		{
			name:      "regex query filters non-matching def",
			reQuery:   regexp.MustCompile("otherFunc"),
			path:      "main.go",
			line:      "func main() {}",
			wantMatch: "",
			wantOK:    false,
		},
		{
			name:      "regex query matches def line",
			reQuery:   regexp.MustCompile("main"),
			path:      "main.go",
			line:      "func main() {}",
			wantMatch: "",
			wantOK:    true,
		},
		{
			name:      "nil query passes all defs",
			reQuery:   nil,
			path:      "main.go",
			line:      "func main() {}",
			wantMatch: "",
			wantOK:    true,
		},
		{
			name:      "non-def line with query",
			reQuery:   regexp.MustCompile("x"),
			path:      "main.go",
			line:      "var x = 1",
			wantMatch: "",
			wantOK:    false,
		},
		{
			name:      "unsupported extension",
			reQuery:   nil,
			path:      "script.rb",
			line:      "def foo",
			wantMatch: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			matcher := getDefinitionMatcher(tt.reQuery, compiledDefs)
			gotMatch, gotOK := matcher(tt.path, tt.line)
			if gotMatch != tt.wantMatch {
				t.Errorf("match = %q, want %q", gotMatch, tt.wantMatch)
			}
			if gotOK != tt.wantOK {
				t.Errorf("ok = %v, want %v", gotOK, tt.wantOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// checkConcurrentSearchError tests
// ---------------------------------------------------------------------------

func TestCheckConcurrentSearchError(t *testing.T) {
	s := &fileSearcher{}

	t.Run("no error pending", func(t *testing.T) {
		errChan := make(chan error, 1)
		if err := s.checkConcurrentSearchError(errChan); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("error present", func(t *testing.T) {
		errChan := make(chan error, 1)
		expectedErr := fmt.Errorf("search failed")
		errChan <- expectedErr
		if err := s.checkConcurrentSearchError(errChan); err != expectedErr {
			t.Errorf("expected %v, got %v", expectedErr, err)
		}
	})
}

// ---------------------------------------------------------------------------
// getPathOrDefault tests
// ---------------------------------------------------------------------------

func TestGetPathOrDefault(t *testing.T) {
	s := &fileSearcher{}

	t.Run("empty path returns dot", func(t *testing.T) {
		got := s.getPathOrDefault("")
		if got != "." {
			t.Errorf("expected '.', got %q", got)
		}
	})

	t.Run("non-empty path preserved", func(t *testing.T) {
		got := s.getPathOrDefault("/some/path")
		if got != "/some/path" {
			t.Errorf("expected '/some/path', got %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// grepDefinitions with concurrent search error
// ---------------------------------------------------------------------------

// mockFS_WalkError overrides Walk to return an error, triggering errChan
type mockFS_WalkError struct {
	persistence.FileSystem
	walkErr error
}

func (m *mockFS_WalkError) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return m.walkErr
}

func TestGrepDefinitions_ConcurrentSearchError(t *testing.T) {
	expectedErr := fmt.Errorf("walk failure")
	fs := &mockFS_WalkError{
		FileSystem: persistencetest.NewPlainOSFileSystem(),
		walkErr:    expectedErr,
	}

	s := &fileSearcher{
		sm:     &toolstest.MockSecurityManager{AllowAll: true},
		fs:     fs,
		policy: infra_persistence.NewWorkspacePolicy(),
	}

	_, err := s.grepDefinitions(context.Background(), map[string]interface{}{"reason": "testing"}, nil)
	if err == nil {
		t.Fatal("expected error from concurrent search")
	}
	if !strings.Contains(err.Error(), "walk failure") {
		t.Errorf("expected 'walk failure' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// grepDefinitions — UnmarshalArgs failure
// ---------------------------------------------------------------------------

func TestGrepDefinitions_UnmarshalArgsFailure(t *testing.T) {
	s := &fileSearcher{
		sm:     &toolstest.MockSecurityManager{AllowAll: true},
		fs:     persistencetest.NewPlainOSFileSystem(),
		policy: infra_persistence.NewWorkspacePolicy(),
	}
	ctx := context.Background()

	// A nested map cannot be unmarshaled into the Path string field.
	args := map[string]interface{}{
		"path": map[string]interface{}{"nested": "value"},
	}

	result, err := s.grepDefinitions(ctx, args, nil)
	if err == nil {
		t.Fatal("expected error from UnmarshalArgs")
	}
	// grepDefinitions returns (tools.ToolResult{}, err) on unmarshal failure.
	if result.Text != "" || result.Error != nil {
		t.Errorf("expected zero-value ToolResult, got %+v", result)
	}
}

// ---------------------------------------------------------------------------
// searchFiles comprehensive tests
// ---------------------------------------------------------------------------

func TestSearchFiles_RegexMode(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	s := &fileSearcher{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	t.Run("regex match", func(t *testing.T) {
		res, err := s.searchFiles(ctx, map[string]interface{}{
			"path":     tempDir,
			"query":    "^hello",
			"is_regex": true,
			"reason":   "testing",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "hello world") {
			t.Errorf("expected match, got: %q", res.Text)
		}
	})

	t.Run("regex no match", func(t *testing.T) {
		res, err := s.searchFiles(ctx, map[string]interface{}{
			"path":     tempDir,
			"query":    "^goodbye",
			"is_regex": true,
			"reason":   "testing",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "0 matches found") {
			t.Errorf("expected '0 matches found', got: %q", res.Text)
		}
	})

	t.Run("invalid regex", func(t *testing.T) {
		_, err := s.searchFiles(ctx, map[string]interface{}{
			"path":     tempDir,
			"query":    "[invalid",
			"is_regex": true,
			"reason":   "testing",
		}, nil)
		if err == nil {
			t.Fatal("expected error for invalid regex")
		}
		if !strings.Contains(err.Error(), "invalid regex") {
			t.Errorf("expected 'invalid regex', got: %v", err)
		}
	})
}

func TestSearchFiles_NoResults(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	s := &fileSearcher{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	res, err := s.searchFiles(ctx, map[string]interface{}{
		"path":   tempDir,
		"query":  "xyznonexistent",
		"reason": "testing",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "0 matches found") {
		t.Errorf("expected '0 matches found', got: %q", res.Text)
	}
}

func TestSearchFiles_ConcurrentSearchError(t *testing.T) {
	fs := &mockFS_WalkError{
		FileSystem: persistencetest.NewPlainOSFileSystem(),
		walkErr:    fmt.Errorf("walk failure"),
	}

	s := &fileSearcher{
		sm:     &toolstest.MockSecurityManager{AllowAll: true},
		fs:     fs,
		policy: infra_persistence.NewWorkspacePolicy(),
	}
	ctx := context.Background()

	_, err := s.searchFiles(ctx, map[string]interface{}{
		"path":   ".",
		"query":  "hello",
		"reason": "testing",
	}, nil)
	if err == nil {
		t.Fatal("expected error from concurrent search walk failure")
	}
	if !strings.Contains(err.Error(), "walk failure") {
		t.Errorf("expected 'walk failure', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// searchFiles — UnmarshalArgs failure
// ---------------------------------------------------------------------------

func TestSearchFiles_UnmarshalArgsFailure(t *testing.T) {
	s := &fileSearcher{
		sm:     &toolstest.MockSecurityManager{AllowAll: true},
		fs:     persistencetest.NewPlainOSFileSystem(),
		policy: infra_persistence.NewWorkspacePolicy(),
	}
	ctx := context.Background()

	// "not_a_bool" cannot be unmarshaled into the IsRegex bool field.
	args := map[string]interface{}{
		"is_regex": "not_a_bool",
	}

	result, err := s.searchFiles(ctx, args, nil)
	if err == nil {
		t.Fatal("expected error from UnmarshalArgs")
	}
	if !reflect.DeepEqual(result, tools.ToolResult{}) {
		t.Errorf("expected zero-value ToolResult, got %+v", result)
	}
}

func TestGetTree_DeepRecursion(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "a/b/c"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "a/b/c/d.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	r := &fileReader{sm: sm, fs: persistencetest.NewPlainOSFileSystem(), policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	t.Run("depth 1 shows two levels", func(t *testing.T) {
		res, err := r.getTree(ctx, map[string]interface{}{"path": tempDir, "max_depth": 1, "reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		// At maxDepth=1 we see depth 0 entries and depth 1 children
		if !strings.Contains(res.Text, "a") {
			t.Errorf("expected 'a' directory in output, got: %s", res.Text)
		}
	})

	t.Run("path default", func(t *testing.T) {
		// When path is empty, it defaults to current dir
		_, err := r.getTree(ctx, map[string]interface{}{"reason": "testing"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Should not error
	})
}

func TestFindFile_WalkError(t *testing.T) {
	fs := &mockFS_WalkError{
		FileSystem: persistencetest.NewPlainOSFileSystem(),
		walkErr:    fmt.Errorf("walk failure"),
	}

	r := &fileReader{
		sm:     &toolstest.MockSecurityManager{AllowAll: true},
		fs:     fs,
		policy: infra_persistence.NewWorkspacePolicy(),
	}
	ctx := context.Background()

	_, err := r.findFile(ctx, map[string]interface{}{
		"path":    ".",
		"pattern": "*.go",
		"reason":  "testing",
	}, nil)
	if err == nil {
		t.Fatal("expected walk error")
	}
	if !strings.Contains(err.Error(), "walk failure") {
		t.Errorf("expected 'walk failure', got: %v", err)
	}
}

func TestReplaceText_ReadError(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")

	// Use mock FS that fails ReadFile
	mfs := &mockFS_ReadError{FileSystem: persistencetest.NewPlainOSFileSystem(), err: fmt.Errorf("read failure")}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	sm.RegisterSafePath(tempDir)
	w := &fileWriter{sm: sm, bm: newBackupManager(sm, persistencetest.NewPlainOSFileSystem(), 10), fs: mfs}
	ctx := context.Background()

	_, err := w.replaceText(ctx, map[string]interface{}{
		"filepath": path,
		"old_text": "old",
		"new_text": "new",
		"reason":   "testing",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to read file") {
		t.Errorf("expected 'failed to read file' error, got: %v", err)
	}
}
