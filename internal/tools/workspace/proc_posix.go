// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package workspace

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// groupKillRetryWindow bounds how long Cancel waits for the child to create
// its process group before falling back to killing the direct process.
//
// Go starts child processes with fork+exec on darwin/BSD, and the child calls
// setpgid after fork but before exec. A context deadline that fires in that
// window makes Kill(-pid) fail with ESRCH (the group does not exist yet), so
// the process tree would otherwise survive cancellation. The window is
// normally microseconds; 200ms covers even heavily preempted children.
const groupKillRetryWindow = 200 * time.Millisecond

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
		// Sending signal to -pid kills the process group. The child creates
		// the group (setpgid) after fork and before exec, so a cancellation
		// landing in that window fails with ESRCH; retry briefly until the
		// group exists or the process is gone, then kill the direct child.
		pid := cmd.Process.Pid
		deadline := time.Now().Add(groupKillRetryWindow)
		for {
			if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
				return nil
			} else if !errors.Is(err, syscall.ESRCH) {
				return err
			}
			// ESRCH: group does not exist yet. If the process itself is
			// gone, it already exited — don't inject a spurious error.
			if err := syscall.Kill(pid, 0); err != nil {
				return os.ErrProcessDone
			}
			if time.Now().After(deadline) {
				// Give up waiting for the group; kill the direct child so
				// the command cannot outlive the deadline.
				return syscall.Kill(pid, syscall.SIGKILL)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
}
