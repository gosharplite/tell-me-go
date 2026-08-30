// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package workspace

import (
	"context"
	"testing"
	"time"
)

func TestTimeoutKillsProcessTree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-spawning test in short mode")
	}
	e := newTestProcessExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _ = e.RunCommand(ctx, []string{"sh", "-c", "echo started; sleep 1 & wait"}, executionConfig{})
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Errorf("RunCommand blocked %v past the 100ms deadline (orphaned grandchild held the pipe)", elapsed)
	}
}
