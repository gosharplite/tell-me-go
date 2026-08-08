// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// defaultFS is the single sanctioned construction site for the OS
// filesystem adapter in this package (issue #1295, ADR-055). Every live
// tool path uses the injected domain port (persistence.FileSystem); this
// fallback exists so the zero-arg newprocessExecutor() keeps working for
// call sites that predate injection. Do not add further
// internal/infrastructure/persistence imports to production files in this
// package — the verify-tools-adapter-import gate enforces the boundary.
var defaultFS = infra_persistence.NewOSFileSystem()
