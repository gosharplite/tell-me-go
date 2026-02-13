package archguard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAnalyze(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a dummy go.mod
	goMod := `module testproj
go 1.22
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	// internal/a: Exports SymbolX (used) and SymbolY (unused)
	dirA := filepath.Join(tmpDir, "internal/a")
	if err := os.MkdirAll(dirA, 0755); err != nil {
		t.Fatal(err)
	}
	codeA := `package a
func SymbolX() {}
func SymbolY() {}
type S struct{}
func (s S) MethodUsed() {}
func (s S) MethodUnused() {}
`
	if err := os.WriteFile(filepath.Join(dirA, "a.go"), []byte(codeA), 0644); err != nil {
		t.Fatal(err)
	}

	// internal/b: Imports internal/a and uses SymbolX
	dirB := filepath.Join(tmpDir, "internal/b")
	if err := os.MkdirAll(dirB, 0755); err != nil {
		t.Fatal(err)
	}
	codeB := `package b
import "testproj/internal/a"
func UseA() {
	a.SymbolX()
	var s a.S
	s.MethodUsed()
}

type ExternalInterface interface {
    Satisfied(string) bool
}
type PointerInterface interface {
    PointerMethod()
}
`
	if err := os.WriteFile(filepath.Join(dirB, "b.go"), []byte(codeB), 0644); err != nil {
		t.Fatal(err)
	}

	// internal/c: Implements ExternalInterface from internal/b
	dirC := filepath.Join(tmpDir, "internal/c")
	if err := os.MkdirAll(dirC, 0755); err != nil {
		t.Fatal(err)
	}
	codeC := `package c
import "testproj/internal/b"
type Impl struct{}
func (i Impl) Satisfied(s string) bool { return true }
type PointerImpl struct{}
func (p *PointerImpl) PointerMethod() {}
`
	if err := os.WriteFile(filepath.Join(dirC, "c.go"), []byte(codeC), 0644); err != nil {
		t.Fatal(err)
	}
	codeCTest := `package c
import "testing"
func TestSomething(t *testing.T) {}
func BenchmarkSomething(b *testing.B) {}
func FuzzSomething(f *testing.F) {}
func ExampleSomething() {}

type LocalStruct struct{}
func (l LocalStruct) InternalUsage() {}
`
	if err := os.WriteFile(filepath.Join(dirC, "c_test.go"), []byte(codeCTest), 0644); err != nil {
		t.Fatal(err)
	}

	codeCUsage := `package c_test
import "testproj/internal/c"
func TestUsage(t *testing.T) {
    var l c.LocalStruct
    l.InternalUsage()
}
`
	if err := os.WriteFile(filepath.Join(dirC, "external_test.go"), []byte(codeCUsage), 0644); err != nil {
		t.Fatal(err)
	}

	// pkg/public: Outside internal, should be ignored by analyzer
	dirPublic := filepath.Join(tmpDir, "pkg/public")
	if err := os.MkdirAll(dirPublic, 0755); err != nil {
		t.Fatal(err)
	}
	codePublic := `package public
func PublicSymbol() {}
`
	if err := os.WriteFile(filepath.Join(dirPublic, "public.go"), []byte(codePublic), 0644); err != nil {
		t.Fatal(err)
	}

	// Run go mod tidy to ensure everything is set up (though it should be fine with local imports)
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, string(out))
	}

	// Change to tmpDir to run Analyze with "./..."
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	findings, err := Analyze(context.Background(), "./...")
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	tests := []struct {
		symbol   string
		category Category
		reason   string
	}{
		{"testproj/internal/a.SymbolX", ArchitecturalBoundary, "Used outside package"},
		{"testproj/internal/a.SymbolY", PrivateCandidate, ""},
		{"(testproj/internal/a.S).MethodUsed", ArchitecturalBoundary, "Used outside package"},
		{"(testproj/internal/a.S).MethodUnused", PrivateCandidate, ""},
		{"(testproj/internal/c.Impl).Satisfied", ArchitecturalBoundary, "Interface Satisfaction"},
		{"(*testproj/internal/c.PointerImpl).PointerMethod", ArchitecturalBoundary, "Interface Satisfaction"},
		{"testproj/internal/c.TestSomething", ArchitecturalBoundary, "Toolchain Hook"},
		{"testproj/internal/c.BenchmarkSomething", ArchitecturalBoundary, "Toolchain Hook"},
		{"testproj/internal/c.FuzzSomething", ArchitecturalBoundary, "Toolchain Hook"},
		{"testproj/internal/c.ExampleSomething", ArchitecturalBoundary, "Toolchain Hook"},
		{"(testproj/internal/c.LocalStruct).InternalUsage", PrivateCandidate, ""},
	}

	for _, tt := range tests {
		found := false
		for _, f := range findings {
			if f.Symbol == tt.symbol {
				found = true
				if f.Category != tt.category {
					t.Errorf("Symbol %s: expected category %s, got %s", tt.symbol, tt.category, f.Category)
				}
				if tt.reason != "" && f.Reason != tt.reason {
					t.Errorf("Symbol %s: expected reason %s, got %s", tt.symbol, tt.reason, f.Reason)
				}
				break
			}
		}
		if !found {
			t.Errorf("Symbol %s not found in findings", tt.symbol)
		}
	}

	// Verify pkg/public is ignored
	for _, f := range findings {
		if filepath.Base(filepath.Dir(f.Symbol)) == "public" {
			t.Errorf("Public package symbol %s should have been ignored", f.Symbol)
		}
	}
}
