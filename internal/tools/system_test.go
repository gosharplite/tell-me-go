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
		{"go test ./...", false},
		{"go build .", false},
		{"go mod tidy", false},
		{"go run main.go", false},
		{"go get github.com/foo/bar", false},
		{"go install github.com/foo/bar", false},
		{"go vet ./...", true},
		{"go fmt ./...", false},
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
		wantErr  bool
	}{
		{"ls -la", []string{"ls", "-la"}, false},
		{"grep \"hello world\" file.txt", []string{"grep", "hello world", "file.txt"}, false},
		{"echo \"\"", []string{"echo", ""}, false},
		{"echo \"   \"", []string{"echo", "   "}, false},
		{"echo \"unclosed quote", nil, true},
		{"", nil, false},
	}

	for _, tt := range tests {
		got, err := splitCommand(tt.cmd)
		if (err != nil) != tt.wantErr {
			t.Errorf("splitCommand(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
			continue
		}
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
