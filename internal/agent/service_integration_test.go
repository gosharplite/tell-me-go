// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatService_Initialization_Failures(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		setup   func() string
		wantErr string
	}{
		{
			name: "Missing Config File",
			setup: func() string {
				return filepath.Join(tmpDir, "missing.yaml")
			},
			wantErr: "no such file or directory",
		},
		{
			name: "Malformed Config File",
			setup: func() string {
				path := filepath.Join(tmpDir, "bad.yaml")
				err := os.WriteFile(path, []byte("{bad-yaml"), 0644)
				require.NoError(t, err)
				return path
			},
			wantErr: "failed to unmarshal expanded yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup()

			// 1. Create a REAL configuration loader (not a mock)
			realLoader := &config.YAMLConfigLoader{}

			// 2. Setup standard dependencies (mocks are fine for non-config deps)
			mockSM := &mockServiceSecurityManager{}
			mockContainer := &mockServiceContainer{}
			mockCapturer := &mockServiceCapturer{}

			service := NewChatService("home", "v1", io.Discard, io.Discard, mockSM, realLoader, mockContainer)

			// 3. Attempt ProcessMessage
			opts := ChatOptions{ConfigPath: path}
			err := service.ProcessMessage(context.Background(), opts, mockCapturer)

			// 4. Assert exact failure
			require.Error(t, err, "expected initialization to fail")
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Contains(t, err.Error(), "error loading config")
		})
	}
}
