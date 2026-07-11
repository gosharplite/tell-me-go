// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var generateFixture = flag.Bool("generate-fixture", false, "generate testdata/dead_code_fixture.json from test cases")

// TestGenerateDeadCodeFixture builds the real indexer from the shared
// test workspace and writes an indexSnapshot to testdata/.
//
// Usage:
//
//	go test -run TestGenerateDeadCodeFixture -generate-fixture ./internal/tools/analysis/
//
// The resulting testdata/dead_code_fixture.json should be committed
// together with testdata/deadcode-fixture-workspace/ which contains
// the actual Go source files. Regenerate whenever test cases in
// getFindOrphanedSymbolsTestCases() change.
func TestGenerateDeadCodeFixture(t *testing.T) {
	if !*generateFixture {
		t.Skip("skipping fixture generation; use -generate-fixture to run")
	}

	// Initialize test cases.
	sharedWSTests = getFindOrphanedSymbolsTestCases()

	// Create a persistent workspace inside testdata/ so the fixture
	// can be loaded without regenerating files.
	wsDir := filepath.Join("testdata", "deadcode-fixture-workspace")
	if err := os.RemoveAll(wsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sharedWSRoot = wsDir
	sharedWSModule = setupSharedWorkspaceAt(wsDir, sharedWSTests)

	var idxErr error
	sharedWSIndexer, idxErr = newIndexer(wsDir)
	if idxErr != nil {
		t.Fatal(idxErr)
	}
	sharedWSIndexer.knownModulePath = sharedWSModule
	if err := sharedWSIndexer.Refresh(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	// Snapshot and write.
	snap := sharedWSIndexer.snapshot()

	outPath := filepath.Join("testdata", "dead_code_fixture.json")
	if err := snap.saveSnapshot(outPath); err != nil {
		t.Fatal(err)
	}

	t.Logf("Fixture written to %s (%d declarations, %d usages, %d impls)",
		outPath, len(snap.Declarations), len(snap.UsagesByName), len(snap.ImplsCache))
	t.Logf("Workspace persisted at %s", wsDir)
}
