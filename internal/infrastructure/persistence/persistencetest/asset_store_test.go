// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistencetest

import (
	"bytes"
	"context"
	"testing"
)

// TestNewAssetStore_NilFS_DoesNotPanic is the AC4 regression test: the
// construction helper must tolerate a nil fs (mirroring the nil-fs
// construction tolerance of the retired history test callers that never
// stored assets). Put/Get are deliberately not called — they would
// dereference nil; the preserved semantics is construction-tolerance only.
func TestNewAssetStore_NilFS_DoesNotPanic(t *testing.T) {
	store := NewAssetStore(nil, "assets")
	if store == nil {
		t.Fatal("NewAssetStore(nil, ...) must return a non-nil store")
	}
}

// TestNewAssetStore_RoundTrip proves the helper wraps the live
// infrastructure adapter: Put stores content-addressable data and Get
// reads it back byte-for-byte.
func TestNewAssetStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	fs := NewPlainOSFileSystem()
	dir := t.TempDir()
	store := NewAssetStore(fs, dir)

	id, err := store.Put(ctx, []byte("asset-data"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if id == "" {
		t.Fatal("Put returned an empty id")
	}

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(got, []byte("asset-data")) {
		t.Errorf("Get returned %q, want %q", got, "asset-data")
	}
}
