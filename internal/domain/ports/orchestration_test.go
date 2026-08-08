// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewSession(t *testing.T) {
	t.Parallel()
	id := "test-session"
	session := NewSession(id, nil)

	if session.ID != id {
		t.Errorf("expected session ID %s, got %s", id, session.ID)
	}
	if session.StartTime.IsZero() {
		t.Error("expected session StartTime to be set")
	}
	if time.Since(session.StartTime) > time.Second {
		t.Error("expected session StartTime to be recent")
	}
}

func TestChatterConfig(t *testing.T) {
	t.Parallel()
	cfg := ChatterConfig{
		ProviderName: "test-provider",
		Model:        "test-model",
		Mode:         "test-mode",
		LogPath:      "/tmp/test.log",
	}
	assert.Equal(t, "test-provider", cfg.ProviderName)
	assert.Equal(t, "test-model", cfg.Model)
	assert.Equal(t, "test-mode", cfg.Mode)
	assert.Equal(t, "/tmp/test.log", cfg.LogPath)
}
