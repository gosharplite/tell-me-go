package analysis

import (
	"path/filepath"
	"testing"
)

func TestIsInSearchPath(t *testing.T) {
	t.Parallel()

	idx := &indexer{} // isInSearchPath is a pure function, no fields needed

	winRoot := filepath.FromSlash("C:/")
	winChild := filepath.FromSlash("C:/a")
	winFile := filepath.FromSlash("C:/a/b.go")
	winSibling := filepath.FromSlash("C:/foobar.go")

	tests := []struct {
		name         string
		target, file string
		want         bool
	}{
		{"exact match", filepath.FromSlash("/a/b.go"), filepath.FromSlash("/a/b.go"), true},
		{"child in subdirectory", filepath.FromSlash("/a"), filepath.FromSlash("/a/b.go"), true},
		{"sibling prefix rejected", filepath.FromSlash("/foo"), filepath.FromSlash("/foobar.go"), false},
		{"root directory", filepath.FromSlash("/"), filepath.FromSlash("/a/b.go"), true},
		{"empty target matches all", "", filepath.FromSlash("/a/b.go"), true},
		{"unrelated path", filepath.FromSlash("/x"), filepath.FromSlash("/y/b.go"), false},
		{"Win root", winRoot, winFile, true},
		{"Win child", winChild, winFile, true},
		{"Win sibling rejected", winChild, winSibling, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idx.isInSearchPath(tt.target, tt.file)
			if got != tt.want {
				t.Errorf("isInSearchPath(%q, %q) = %v; want %v", tt.target, tt.file, got, tt.want)
			}
		})
	}
}
