// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package security

import (
	"os"
	"path/filepath"
)

func getSystemDirectories() []string {
	var sensitive []string
	// HARDCODED FALLBACKS (Defense in Depth)
	fallbacks := []string{
		`C:\Windows`,
		`C:\Program Files`,
		`C:\Program Files (x86)`,
		`C:\ProgramData`,
	}
	for _, f := range fallbacks {
		abs, err := filepath.Abs(filepath.Clean(f))
		if err == nil {
			sensitive = append(sensitive, abs)
		}
	}

	// Populate sensitive Windows directories via environment variables
	winDirs := []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramData", "WINDIR"}
	for _, env := range winDirs {
		if val := os.Getenv(env); val != "" {
			abs, err := filepath.Abs(filepath.Clean(val))
			if err == nil {
				sensitive = append(sensitive, abs)
			}
		}
	}
	return sensitive
}

func isCaseSensitive() bool {
	return false
}

func getExtraTempDirs() []string {
	return nil
}
