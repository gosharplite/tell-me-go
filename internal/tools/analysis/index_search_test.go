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
		{"exact match", "/a/b.go", "/a/b.go", true},
		{"child in subdirectory", "/a", "/a/b.go", true},
		{"sibling prefix rejected", "/foo", "/foobar.go", false},
		{"root directory", "/", "/a/b.go", true},
		{"empty target matches all", "", "/a/b.go", true},
		{"unrelated path", "/x", "/y/b.go", false},
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
