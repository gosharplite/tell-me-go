//go:build windows
// +build windows

package workspace

import (
	"strings"
	"testing"
)

func TestWindowsShellWrapper_UTF8Forcing(t *testing.T) {
	wrapper := &windowsShellWrapper{}

	t.Run("PowerShell forced to UTF-8", func(t *testing.T) {
		cmd := "Get-ChildItem"
		parts := []string{"Get-ChildItem"}
		wrapped := wrapper.Wrap(cmd, parts)

		foundUTF8 := false
		for _, arg := range wrapped {
			if strings.Contains(arg, "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8;") {
				foundUTF8 = true
				break
			}
		}

		if !foundUTF8 {
			t.Errorf("expected wrapped command to contain UTF-8 forcing, got %v", wrapped)
		}
	})

	t.Run("New PowerShell aliases", func(t *testing.T) {
		tests := []struct {
			cmd  string
			want bool
		}{
			{"ps", true},
			{"kill", true},
			{"cat", true},
			{"curl", true},
			{"wget", true},
			{"ls", false}, // ls is translated by windowsTranslator to cmd /c dir by default, not wrapped in PS unless it has shell features
		}

		for _, tt := range tests {
			t.Run(tt.cmd, func(t *testing.T) {
				got := wrapper.isPowerShellIndicator(tt.cmd, []string{tt.cmd})
				if got != tt.want {
					t.Errorf("isPowerShellIndicator(%q) = %v, want %v", tt.cmd, got, tt.want)
				}
			})
		}
	})
}
