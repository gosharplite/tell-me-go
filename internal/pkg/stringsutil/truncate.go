// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package stringsutil

// TruncateOutput limits a string to a maximum number of lines, appending a truncation message if needed.
// It is designed to be memory-efficient by avoiding a full split of the string.
func TruncateOutput(output string, maxLines int) string {
	if output == "" {
		return ""
	}
	if maxLines <= 0 {
		return "\n... (Output truncated) ..."
	}

	count := 0
	for i := 0; i < len(output); i++ {
		if output[i] == '\n' {
			count++
			if count >= maxLines && i < len(output)-1 {
				return output[:i] + "\n... (Output truncated) ..."
			}
		}
	}
	return output
}
