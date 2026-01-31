package tools

import (
	"context"
	"fmt"
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

	results, err := grepDefinitionsGo(context.Background(), tmpDir, "Foo")
	if err != nil {
		t.Fatalf("grepDefinitionsGo failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}

func TestASTCacheEviction(t *testing.T) {
	cache := newASTCache()
	cache.maxSize = 2 // Small limit

	tmpDir := t.TempDir()

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("file%d.go", i)
		path := filepath.Join(tmpDir, name)
		content := fmt.Sprintf("package main\nfunc F%d(){}", i)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		_, _, err := cache.get(path)
		if err != nil {
			t.Fatalf("Failed to get %s: %v", name, err)
		}
	}

	cache.mu.Lock()
	size := len(cache.files)
	cache.mu.Unlock()

	if size > 2 {
		t.Errorf("Cache size %d exceeds max limit 2", size)
	}
}
