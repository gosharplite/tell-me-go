// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package command

import (
	"testing"
)

func TestRegistry(t *testing.T) {
	// Dummy factory
	factory1 := func(ctx *Context) Command { return nil }
	factory2 := func(ctx *Context) Command { return nil }

	// Test Register and Get
	Register("test1", factory1)
	f, err := Get("test1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if f == nil {
		t.Error("expected factory, got nil")
	}

	// Test duplicate registration (overwrite)
	Register("test1", factory2)
	f, err = Get("test1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if f == nil {
		t.Error("expected factory, got nil")
	}

	// Test non-existent command
	_, err = Get("non-existent")
	if err == nil {
		t.Error("expected error for non-existent command, got nil")
	}
}
