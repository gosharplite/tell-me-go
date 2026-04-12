// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package security

func getSystemDirectories() []string {
	return []string{
		"/etc",
		"/usr",
		"/bin",
		"/sbin",
		"/var",
		"/root",
		"/boot",
		"/dev",
		"/proc",
		"/sys",
		"/private/etc",
		"/private/var",
	}
}

func isCaseSensitive() bool {
	return true
}

func getExtraTempDirs() []string {
	return []string{"/tmp", "/private/tmp"}
}
