package matcher

import "strings"

// IsSubsequence checks if query is a subsequence of target in a case-insensitive manner.
func IsSubsequence(query, target string) bool {
	if query == "" {
		return true
	}

	query = strings.ToLower(query)
	target = strings.ToLower(target)

	queryRunes := []rune(query)
	targetRunes := []rune(target)

	qIdx := 0
	for tIdx := 0; tIdx < len(targetRunes); tIdx++ {
		if queryRunes[qIdx] == targetRunes[tIdx] {
			qIdx++
		}
		if qIdx == len(queryRunes) {
			return true
		}
	}

	return false
}
