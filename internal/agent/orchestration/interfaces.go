// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"flag"
)

// Capturer defines the interface for UI interactions that the orchestrator needs.
type Capturer interface {
	IsTTY(v any) bool
	CapturePrompt(ctx context.Context, fs *flag.FlagSet, lastN int, raw bool) (string, error)
}
