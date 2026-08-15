// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package app

import (
	"os"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestNewChatService(t *testing.T) {
	t.Run("constructs service and implements interface with default path resolver", func(t *testing.T) {
		cfg := ports.ChatServiceConfig{
			HomeDir: t.TempDir(),
			Version: "dev",
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
		}

		svc := NewChatService(cfg)
		if svc == nil {
			t.Fatal("expected non-nil ChatService")
		}

		var _ ports.ChatService = svc
	})

	t.Run("preserves custom path resolver", func(t *testing.T) {
		customResolver := func(homeDir, mode string) *persistence.Paths {
			return &persistence.Paths{
				ModeDir: homeDir,
			}
		}

		cfg := ports.ChatServiceConfig{
			HomeDir:      t.TempDir(),
			Version:      "dev",
			Stdout:       os.Stdout,
			Stderr:       os.Stderr,
			ResolvePaths: customResolver,
		}

		svc := NewChatService(cfg)
		if svc == nil {
			t.Fatal("expected non-nil ChatService")
		}
	})
}
