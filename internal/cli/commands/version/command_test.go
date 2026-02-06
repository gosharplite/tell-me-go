// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package version

import (
	"bytes"
	"context"
	"testing"
)

func TestExecute(t *testing.T) {
	var out bytes.Buffer
	cmd := &Command{
		Version: "1.2.3",
		Stdout:  &out,
	}

	err := cmd.Execute(context.Background(), []string{"bin", "-v"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	expected := "tell-me-go version 1.2.3\n"
	if out.String() != expected {
		t.Errorf("expected %q, got %q", expected, out.String())
	}
}
