// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package auth

import (
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// defaultFS is the single sanctioned construction site for the OS
// filesystem adapter in this package (issue #1350, ADR-055). Every live
// auth path uses the injected domain port (persistence.FileSystem); this
// fallback exists so the zero-arg NewVertexAuth() keeps working for call
// sites that predate injection. Do not add further
// internal/infrastructure/persistence imports to production files in this
// package.
var defaultFS = infra_persistence.NewOSFileSystem()
