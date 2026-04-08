// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import "errors"

var (
	// ErrInfraInit indicates a failure during infrastructure component initialization.
	ErrInfraInit = errors.New("infrastructure initialization failed")

	// ErrInfrastructureUnavailable indicates that a required infrastructure service is not reachable.
	ErrInfrastructureUnavailable = errors.New("infrastructure unavailable")
)
