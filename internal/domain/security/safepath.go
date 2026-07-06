// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "time"

// SafePathMode represents the access mode of an authorized path boundary.
type SafePathMode string

const (
	// SafePathReadWrite allows both reading and writing within the path boundary.
	SafePathReadWrite SafePathMode = "readwrite"
	// SafePathRead allows only reading within the path boundary.
	SafePathRead SafePathMode = "read"
)

// SafePath is a directory or file boundary authorized for Tool operations.
// Once authorized, the path persists across Sessions. It enforces the
// safepath-absolute invariant: paths are always stored in canonical absolute form.
type SafePath struct {
	Path         string
	Mode         SafePathMode
	AuthorizedAt time.Time
}
