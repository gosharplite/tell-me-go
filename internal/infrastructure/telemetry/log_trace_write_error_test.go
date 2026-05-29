// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"os"
	"testing"

	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
)

// TestLogTrace_WriteError_DevFull covers the f.Write failure branch in logTrace
// by writing to /dev/full, a device that accepts opens but fails writes with ENOSPC.
func TestLogTrace_WriteError_DevFull(t *testing.T) {
	if _, err := os.Stat("/dev/full"); os.IsNotExist(err) {
		t.Skip("/dev/full does not exist on this system")
	}

	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}

	// logTrace calls os.OpenFile(traceFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644).
	// On /dev/full:
	//   - O_CREATE is a no-op for device files.
	//   - O_APPEND|O_WRONLY succeeds.
	//   - The subsequent f.Write returns ENOSPC, hitting the target branch.
	logTrace(context.Background(), "/dev/full", trace)

	// If we reach here without panicking, the write-error branch was exercised.
}
