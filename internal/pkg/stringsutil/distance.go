// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package stringsutil

// LevenshteinDistance calculates the Levenshtein distance between two strings.
func LevenshteinDistance(s, t string) int {
	s1, s2 := []rune(s), []rune(t)
	m, n := len(s1), len(s2)

	if m < n {
		s1, s2 = s2, s1
		m, n = n, m
	}

	prev := make([]int, n+1)
	for j := 0; j <= n; j++ {
		prev[j] = j
	}

	curr := make([]int, n+1)
	for i := 1; i <= m; i++ {
		curr[0] = i
		for j := 1; j <= n; j++ {
			substitutionCost := 0
			if s1[i-1] != s2[j-1] {
				substitutionCost = 1
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+substitutionCost)
		}
		copy(prev, curr)
	}
	return prev[n]
}
