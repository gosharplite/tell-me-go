// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package env

// ResolveHomeDir determines the application's home directory based on environment variables
// and system defaults. OS-level dependencies are injected to allow full test coverage.
func ResolveHomeDir(getenv func(string) string, userHomeDir func() (string, error)) string {
	if homeDir := getenv("TELL_ME_HOME"); homeDir != "" {
		return homeDir
	}
	if homeDir := getenv("AIT_HOME"); homeDir != "" {
		return homeDir
	}
	homeDir, err := userHomeDir()
	if err != nil || homeDir == "" {
		return "."
	}
	return homeDir
}
