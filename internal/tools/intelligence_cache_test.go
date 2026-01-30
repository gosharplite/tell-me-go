package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestASTCache(t *testing.T) {
	// Create a temp directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.go")
	content := `package main

func Hello() {
	println("Hello")
}
`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Initialize cache
	cache := newASTCache()

	// First fetch
	f1, _, err := cache.get(filePath)
	if err != nil {
		t.Fatalf("First get failed: %v", err)
	}

	// Second fetch - should be same object
	f2, _, err := cache.get(filePath)
	if err != nil {
		t.Fatalf("Second get failed: %v", err)
	}

	if f1 != f2 {
		t.Error("Expected same *ast.File object from cache, got different pointers")
	}

	// Modify file
	time.Sleep(100 * time.Millisecond) // Ensure mtime changes
	newContent := `package main

func Hello() {
	println("Hello World")
}
`
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Third fetch - should be new object
	f3, _, err := cache.get(filePath)
	if err != nil {
		t.Fatalf("Third get failed: %v", err)
	}

	if f1 == f3 {
		t.Error("Expected new *ast.File object after file modification, got same pointer")
	}
}

func TestGrepDefinitionsGo_WithCache(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.go")
	content := `package main

func Foo() {}
type Bar struct{}
`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// We need to ensure the global cache is used or inject it.
	// Since we are modifying grepDefinitionsGo to use the global cache,
	// we can just run it.

	// Reset global cache for test if accessible, or just rely on it working.
	// We can't easily reset a private global from here unless we add a helper.
	// But it shouldn't matter for correctness.

	results, err := grepDefinitionsGo(tmpDir, "Foo")
	if err != nil {
		t.Fatalf("grepDefinitionsGo failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}
