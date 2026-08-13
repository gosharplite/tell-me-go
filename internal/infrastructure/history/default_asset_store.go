// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// defaultAssetStore is the single sanctioned construction site for the
// concrete infrastructure AssetStore in this package (issue #1350, item 5).
// The production DI root injects the domain port via NewManagerWithAssetStore;
// this fallback keeps the zero-churn NewManager / newJSONLStore constructors
// working for the ~150 test/bench call sites that predate injection. Do not
// construct infrapersistence.NewAssetStore anywhere else in this package.
func defaultAssetStore(fs persistence.FileSystem, assetDir string) persistence.AssetStore {
	return infrapersistence.NewAssetStore(fs, assetDir)
}
