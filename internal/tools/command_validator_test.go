// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"testing"
)

func TestCommandValidator_IsSafe(t *testing.T) {
	sm := NewSecurityManager()
	v := NewCommandValidator(sm)
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
		{"go clean", false},
	}

	for _, tt := range tests {
		got, _ := v.IsSafe(tt.cmd)
		if got != tt.want {
			t.Errorf("IsSafe(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestCommandValidator_Split(t *testing.T) {
	v := &CommandValidator{}
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
		got, err := v.Split(tt.cmd)
		if (err != nil) != tt.wantErr {
			t.Errorf("Split(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
			continue
		}
		if len(got) != len(tt.expected) {
			t.Errorf("Split(%q) length = %d, want %d", tt.cmd, len(got), len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("Split(%q)[%d] = %q, want %q", tt.cmd, i, got[i], tt.expected[i])
			}
		}
	}
}
