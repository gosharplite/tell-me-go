// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package version

import (
	"bytes"
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/cli/command"
)

func TestCommand_Execute(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected string
	}{
		{
			name:     "simple version",
			version:  "1.0.0",
			expected: "tell-me-go version 1.0.0\n",
		},
		{
			name:     "dev version",
			version:  "v1.91.0-dev",
			expected: "tell-me-go version v1.91.0-dev\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := &Command{
				Version: tt.version,
				Stdout:  &out,
			}

			err := cmd.Execute(context.Background(), nil)
			if err != nil {
				t.Fatalf("Execute() unexpected error: %v", err)
			}

			if out.String() != tt.expected {
				t.Errorf("Execute() got = %q, want %q", out.String(), tt.expected)
			}
		})
	}
}

func TestCommandFactory(t *testing.T) {
	factory, err := command.Get("version")
	if err != nil {
		t.Fatalf("command.Get(\"version\") error = %v", err)
	}

	ctx := &command.Context{
		Version: "1.2.3-test",
		Stdout:  &bytes.Buffer{},
	}

	cmd := factory(ctx)
	vCmd, ok := cmd.(*Command)
	if !ok {
		t.Fatalf("factory did not return *Command, got %T", cmd)
	}

	if vCmd.Version != ctx.Version {
		t.Errorf("expected version %s, got %s", ctx.Version, vCmd.Version)
	}
	if vCmd.Stdout != ctx.Stdout {
		t.Errorf("expected stdout %p, got %p", ctx.Stdout, vCmd.Stdout)
	}
}
