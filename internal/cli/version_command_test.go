// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"testing"
)

func TestVersionCommand_Execute(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			var out bytes.Buffer
			cmd := &versionCommand{
				Version: tt.version,
				Stdout:  &out,
			}

			err := cmd.Execute(stdctx.Background(), nil)
			if err != nil {
				t.Fatalf("Execute() unexpected error: %v", err)
			}

			if out.String() != tt.expected {
				t.Errorf("Execute() got = %q, want %q", out.String(), tt.expected)
			}
		})
	}
}

func TestVersionCommandFactory(t *testing.T) {
	t.Parallel()
	factory, err := get("version")
	if err != nil {
		t.Fatalf("get(\"version\") error = %v", err)
	}

	ctx := &context{
		Version: "1.2.3-test",
		Stdout:  &bytes.Buffer{},
	}

	cmd := factory(ctx)
	vCmd, ok := cmd.(*versionCommand)
	if !ok {
		t.Fatalf("factory did not return *versionCommand, got %T", cmd)
	}

	if vCmd.Version != ctx.Version {
		t.Errorf("expected version %s, got %s", ctx.Version, vCmd.Version)
	}
	if vCmd.Stdout != ctx.Stdout {
		t.Errorf("expected stdout %p, got %p", ctx.Stdout, vCmd.Stdout)
	}
}
