// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistencetest

import (
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// NewAssetStore builds the real infrastructure AssetStore adapter against fs
// for test use. It is the single test-side construction site for the adapter
// (issue #1469): production constructs AssetStore only in
// internal/infrastructure/di. Construction must NOT dereference fs — test
// sites that historically passed a nil fs to history.NewManager (e.g.
// internal/infrastructure/di/container_test.go, factory_error_test.go) keep
// that tolerance by passing nil here too; the store's fs is touched only
// when Put/Get are actually called.
func NewAssetStore(fs persistence.FileSystem, baseDir string) persistence.AssetStore {
	return infrapersistence.NewAssetStore(fs, baseDir)
}
