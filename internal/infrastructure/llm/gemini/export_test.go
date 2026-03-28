// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/gemini"
)

// This silences the dead-code warning for ResetConnections by providing a cross-package reference.
// The resilientClient uses structural typing to invoke this, which AST scanners often miss.
var _ = (*gemini.Client).ResetConnections
