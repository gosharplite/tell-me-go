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
	e := newprocessExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _ = e.RunCommand(ctx, []string{"sh", "-c", "echo started; sleep 2 & wait"}, executionConfig{})
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("RunCommand blocked %v past the 100ms deadline (orphaned grandchild held the pipe)", elapsed)
	}
}
