// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build arch

package analysis

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var strictArch = flag.Bool("strict-arch", true, "fail test on architecture violations")

func TestVerifyRealArchitecture(t *testing.T) {
	m := &architectureManager{
		SP:  &mockSecurityProvider{},
		idx: getRealArchitectureIndexer(t),
	}

	res, err := m.VerifyArchitecture(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("VerifyArchitecture failed: %v", err)
	}

	if strings.Contains(res.Text, "FAILED") {
		if *strictArch {
			t.Errorf("Architecture validation FAILED:\n%s", res.Text)
		} else {
			t.Logf("Architecture validation FAILED:\n%s", res.Text)
		}
	}
}

// findModuleRoot walks up from the current working directory until it finds
// a go.mod file, returning the absolute path to the module root.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

// getRealArchitectureIndexer creates an indexer against the live project
// module root. It is used only by TestVerifyRealArchitecture, which
// explicitly validates the real project's architecture.
func getRealArchitectureIndexer(tb testing.TB) *indexer {
	tb.Helper()
	dir, err := findModuleRoot()
	if err != nil {
		tb.Fatalf("failed to find module root for architecture indexer: %v", err)
	}
	idx, err := newIndexer(dir)
	if err != nil {
		tb.Fatalf("failed to create architecture indexer: %v", err)
	}
	if err := idx.Refresh(context.Background(), nil); err != nil {
		tb.Fatalf("failed to refresh architecture indexer: %v", err)
	}
	return idx
}
