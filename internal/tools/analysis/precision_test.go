package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeadCodeAnalyzer_Precision(t *testing.T) {
	t.Parallel()
	// Setup temporary workspace
	tmpDir, err := os.MkdirTemp("", "precision-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create go.mod
	goMod := `module precision.test

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	// Create pkg_a
	pkgADir := filepath.Join(tmpDir, "pkg_a")
	if err := os.MkdirAll(pkgADir, 0755); err != nil {
		t.Fatalf("failed to create pkg_a dir: %v", err)
	}
	pkgAFunc := `package pkg_a

import "fmt"

func Execute() {
	fmt.Println("Executing A")
}
`
	if err := os.WriteFile(filepath.Join(pkgADir, "a.go"), []byte(pkgAFunc), 0644); err != nil {
		t.Fatalf("failed to create pkg_a/a.go: %v", err)
	}

	// Create pkg_b
	pkgBDir := filepath.Join(tmpDir, "pkg_b")
	if err := os.MkdirAll(pkgBDir, 0755); err != nil {
		t.Fatalf("failed to create pkg_b dir: %v", err)
	}
	pkgBFunc := `package pkg_b

import "fmt"

func Execute() {
	fmt.Println("Executing B")
}
`
	if err := os.WriteFile(filepath.Join(pkgBDir, "b.go"), []byte(pkgBFunc), 0644); err != nil {
		t.Fatalf("failed to create pkg_b/b.go: %v", err)
	}

	// Create main.go calling pkg_a.Execute but NOT pkg_b.Execute
	mainFunc := `package main

import "precision.test/pkg_a"

func main() {
	pkg_a.Execute()
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainFunc), 0644); err != nil {
		t.Fatalf("failed to create main.go: %v", err)
	}

	// Run Dead Code Analyzer
	sp := &mockSecurityProvider{}
	idx, err := newIndexer(tmpDir)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	analyzer := newDeadCodeAnalyzer(sp, idx)
	result, err := analyzer.FindOrphanedSymbols(context.Background(), map[string]interface{}{
		"path": tmpDir,
	})
	if err != nil {
		t.Fatalf("FindOrphanedSymbols failed: %v", err)
	}

	// Assertions
	// pkg_b.Execute should be DEAD
	// pkg_a.Execute should NOT be reported as DEAD (it's used in main)
	// Actually pkg_a.Execute might be reported as PRIVATE if it's only used in its own module but not externally,
	// but here main is in the same module.

	// Wait, the analyzer identifies EXPORTED symbols with zero inbound references within the module.
	// pkg_a.Execute is used in main.go (same module).
	// pkg_b.Execute is NOT used.

	if !strings.Contains(result.Text, "[DEAD] Execute (Function)") || !strings.Contains(result.Text, "Package: precision.test/pkg_b") {
		t.Errorf("Expected pkg_b.Execute to be identified as DEAD. Result:\n%s", result.Text)
	}

	if strings.Contains(result.Text, "Package: precision.test/pkg_a") && strings.Contains(result.Text, "[DEAD] Execute") {
		t.Errorf("pkg_a.Execute should NOT be identified as DEAD as it is used in main.go. Result:\n%s", result.Text)
	}
}
