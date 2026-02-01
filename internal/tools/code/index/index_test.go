package index

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIndexer_FindImplementors(t *testing.T) {
	// Create a temporary directory for test code
	tmpDir, err := os.MkdirTemp("", "indexer_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a dummy go.mod
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create an interface and an implementation
	code := `
package test

type Shape interface {
	Area() float64
}

type Circle struct {
	Radius float64
}

func (c Circle) Area() float64 {
	return 3.14 * c.Radius * c.Radius
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)
	if err != nil {
		t.Fatal(err)
	}

	idx, err := NewIndexer(tmpDir)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}

	err = idx.Refresh(context.Background())
	if err != nil {
		t.Fatalf("failed to refresh indexer: %v", err)
	}

	implementors, err := idx.FindImplementors(context.Background(), "Shape")
	if err != nil {
		t.Fatalf("failed to find implementors: %v", err)
	}

	found := false
	for _, imp := range implementors {
		if imp.Name == "Circle" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected to find Circle as implementor of Shape")
	}
}


func TestIndexer_Concurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "indexer_concurrency_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	code := "package test\ntype X interface { Foo() }\ntype Y struct{}\nfunc (y Y) Foo() {}"
	err = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)
	if err != nil {
		t.Fatal(err)
	}

	idx, err := NewIndexer(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			idx.Refresh(ctx)
		}()
		go func() {
			defer wg.Done()
			idx.Lookup(ctx, "X")
		}()
		go func() {
			defer wg.Done()
			idx.FindImplementors(ctx, "X")
		}()
	}
	wg.Wait()
}
