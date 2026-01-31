// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"os"

	"github.com/gosharplite/tell-me-go/internal/security"
)

// SecurityManager is a type alias to the centralized security manager.
type SecurityManager = security.SecurityManager

// NewSecurityManager initializes a new SecurityManager.
func NewSecurityManager() *SecurityManager {
	return security.NewSecurityManager(os.Stdin)
}
