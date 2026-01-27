// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"testing"
)

func TestIsSafeCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"ls -la", true},
		{"pwd", true},
		{"cat /etc/passwd", false},
		{"cat ../../../../../../../../../../etc/passwd", false},
		{"echo $HOME", false},
		{"cat \"/etc/passwd\"", false},
		{"grep pattern file.txt", true},
		{"grep pattern /etc/passwd", false},
	}

	for _, tt := range tests {
		got := isSafeCommand(tt.cmd)
		if got != tt.want {
			t.Errorf("isSafeCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}
