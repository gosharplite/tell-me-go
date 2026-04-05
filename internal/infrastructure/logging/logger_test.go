// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"bytes"
	"testing"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name        string
		debugEnv    string
		expectDebug bool
	}{
		{
			name:        "Default to Warn level",
			debugEnv:    "",
			expectDebug: false,
		},
		{
			name:        "Debug level enabled",
			debugEnv:    "1",
			expectDebug: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(&buf, tt.debugEnv)

			logger.Debug("debug message")
			logger.Warn("warn message")

			output := buf.String()
			hasDebug := bytes.Contains([]byte(output), []byte("DEBUG"))

			if tt.expectDebug && !hasDebug {
				t.Errorf("expected DEBUG log output, got: %s", output)
			}
			if !tt.expectDebug && hasDebug {
				t.Errorf("did not expect DEBUG log output, got: %s", output)
			}
		})
	}
}
