// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package env

// ResolveHomeDir determines the application's home directory based on environment variables
// and system defaults. OS-level dependencies are injected to allow full test coverage.
func ResolveHomeDir(getenv func(key string) string, userHomeDir func() (string, error)) string {
	homeDir := getenv("TELL_ME_HOME")
	if homeDir != "" {
		return homeDir
	}
	homeDir = getenv("AIT_HOME")
	if homeDir != "" {
		return homeDir
	}
	homeDir, err := userHomeDir()
	if err != nil || homeDir == "" {
		return "."
	}
	return homeDir
}
