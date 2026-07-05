// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package workspace

import (
	"os/exec"
	"syscall"
)

// configureProcAttrs sets up platform-specific process attributes and
// cancellation behavior for exec.Cmd to ensure the entire process tree
// is terminated on timeout or cancellation, rather than just the direct child.
func configureProcAttrs(cmd *exec.Cmd) {
	// Put the process into its own process group
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Override default context cancellation to kill the whole process group
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Sending signal to -pid kills the process group
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
