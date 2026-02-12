// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"
)

func TestRegistry(t *testing.T) {
	// Dummy factory
	factory1 := func(ctx *context) command { return nil }
	factory2 := func(ctx *context) command { return nil }

	// Test Register and Get
	register("test1", factory1)
	f, err := get("test1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if f == nil {
		t.Error("expected factory, got nil")
	}

	// Test duplicate registration (overwrite)
	register("test1", factory2)
	f, err = get("test1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if f == nil {
		t.Error("expected factory, got nil")
	}

	// Test non-existent command
	_, err = get("non-existent")
	if err == nil {
		t.Error("expected error for non-existent command, got nil")
	}
}
