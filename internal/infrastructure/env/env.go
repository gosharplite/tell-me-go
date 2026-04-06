// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package env

import "os"

// ResolveHomeDir determines the application's home directory based on environment variables
// and system defaults. OS-level dependencies are injected to allow full test coverage.
func ResolveHomeDir(userHomeDir func() (string, error)) string {
	if homeDir := os.Getenv("TELL_ME_HOME"); homeDir != "" {
		return homeDir
	}
	if homeDir := os.Getenv("AIT_HOME"); homeDir != "" {
		return homeDir
	}
	homeDir, err := userHomeDir()
	if err != nil || homeDir == "" {
		return "."
	}
	return homeDir
}
