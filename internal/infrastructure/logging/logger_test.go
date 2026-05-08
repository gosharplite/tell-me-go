// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"bytes"
	"testing"
)

func TestNewLogger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		isDebug     bool
		expectDebug bool
	}{
		{
			name:        "Default to Warn level",
			isDebug:     false,
			expectDebug: false,
		},
		{
			name:        "Debug level enabled",
			isDebug:     true,
			expectDebug: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := NewLogger(&buf, tt.isDebug)

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

