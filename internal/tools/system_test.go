// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"testing"
)

func TestIsSafeCommand(t *testing.T) {
	sm := NewSecurityManager()
	m := &systemManager{sm: sm}
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
		{"go test ./...", true},
		{"go build .", true},
		{"go mod tidy", true},
		{"go run main.go", true},
		{"go get github.com/foo/bar", true},
		{"go install github.com/foo/bar", true},
		{"go vet ./...", true},
		{"go fmt ./...", true},
		{"go clean", false}, // Not in whitelist
	}

	for _, tt := range tests {
		got := m.isSafeCommand(tt.cmd)
		if got != tt.want {
			t.Errorf("isSafeCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		cmd      string
		expected []string
	}{
		{"ls -la", []string{"ls", "-la"}},
		{"grep \"hello world\" file.txt", []string{"grep", "hello world", "file.txt"}},
		{"echo \"\"", []string{"echo", ""}},
		{"echo \"   \"", []string{"echo", "   "}},
		{"", nil},
	}

	for _, tt := range tests {
		got := splitCommand(tt.cmd)
		if len(got) != len(tt.expected) {
			t.Errorf("splitCommand(%q) length = %d, want %d", tt.cmd, len(got), len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("splitCommand(%q)[%d] = %q, want %q", tt.cmd, i, got[i], tt.expected[i])
			}
		}
	}
}
