// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package process

import (
	"os/exec"
	"strconv"
)

// configureProcAttrs sets up platform-specific process attributes and
// cancellation behavior for exec.Cmd to ensure the entire process tree
// is terminated on timeout or cancellation, rather than just the direct child.
func configureProcAttrs(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// /F = Forcefully terminate
		// /T = Terminate child processes (the tree)
		// /PID = Process ID
		return exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
}
