// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/ast"
	"go/token"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransaction_MemoryFS_LoadTransformCommit is the PROOF PROPERTY for
// issue #1295 PR1: the transaction's read and write paths must be fully
// routed through the injected persistence.FileSystem port.
//
// Against pre-change code this test FAILS: LoadFile parses the real disk,
// so the in-memory path cannot be read (the transaction was OS-bound).
// Post-change, LoadFile reads via tx.fs.ReadFile and Commit writes via
// tx.fs.AtomicWrite, so the full Load -> Transform -> Commit cycle runs
// against the in-memory filesystem. Scope is the transaction unit only —
// NOT the tools (RenameSymbol/MoveDefinition stay OS-bound via Glob).
func TestTransaction_MemoryFS_LoadTransformCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mfs := persistence.NewMockFileSystem()
	tx := newTransactionWithFS(mfs)

	const path = "memory:/src/main.go"
	const content = "package main\n\nfunc main() {}\n"
	require.NoError(t, mfs.WriteFile(ctx, path, []byte(content), 0644))

	// Read path: must come from the injected FS, not the OS.
	f, err := tx.LoadFile(ctx, path)
	require.NoError(t, err, "LoadFile must parse through the injected FileSystem")
	require.NotNil(t, f)

	// Transform: no-op that succeeds (round-trips the parsed file).
	tx.Add(&mockTransform{
		applyFn: func(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
			return nil
		},
	})

	// Write path: must go through tx.fs.AtomicWrite, not OpenFile on the OS.
	require.NoError(t, tx.Commit(ctx), "Commit must write through the injected FileSystem")

	got, err := mfs.ReadFile(ctx, path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "package main")
	assert.Contains(t, string(got), "func main()")
}

// TestNewTransactionWithFS_NilPanics pins the contract-violation guard on
// the injection seam (ADR-055 as amended by #1465: no package default —
// construction is explicit).
func TestNewTransactionWithFS_NilPanics(t *testing.T) {
	t.Parallel()
	require.PanicsWithValue(t,
		"nil FileSystem to newTransactionWithFS: inject a non-nil persistence.FileSystem (no package default exists)",
		func() { newTransactionWithFS(nil) },
	)
}
