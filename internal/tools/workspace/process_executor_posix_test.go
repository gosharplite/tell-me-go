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
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	_, _ = e.RunCommand(ctx, []string{"sh", "-c", "echo started; sleep 6 & wait"}, executionConfig{})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("RunCommand blocked %v past the 1s deadline (orphaned grandchild held the pipe)", elapsed)
	}
}
