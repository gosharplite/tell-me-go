// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "errors"

// ErrSandboxViolation is returned when an action violates the sandbox boundaries (e.g., restricted paths or forbidden commands).
var ErrSandboxViolation = errors.New("security violation: path outside allowed boundaries")
