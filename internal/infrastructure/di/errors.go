// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import "errors"

var (
	// errInfraInit indicates a failure during infrastructure component initialization.
	errInfraInit = errors.New("infrastructure initialization failed")
)
