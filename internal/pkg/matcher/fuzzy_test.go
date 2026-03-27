package matcher

import "testing"

func TestIsSubsequence(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		target   string
		expected bool
	}{
		{
			name:     "Empty query",
			query:    "",
			target:   "anything",
			expected: true,
		},
		{
			name:     "Exact match",
			query:    "main.go",
			target:   "main.go",
			expected: true,
		},
		{
			name:     "Case insensitivity",
			query:    "MGO",
			target:   "main.go",
			expected: true,
		},
		{
			name:     "Valid subsequence - mgo in main.go",
			query:    "mgo",
			target:   "main.go",
			expected: true,
		},
		{
			name:     "Valid subsequence - srv in chat_service.go",
			query:    "srv",
			target:   "chat_service.go",
			expected: true,
		},
		{
			name:     "Invalid subsequence - characters exist but out of order",
			query:    "og",
			target:   "main.go",
			expected: false,
		},
		{
			name:     "Missing characters",
			query:    "xyz",
			target:   "main.go",
			expected: false,
		},
		{
			name:     "Case-insensitive match with mixed casing",
			query:    "cHaT",
			target:   "ChatService",
			expected: true,
		},
		{
			name:     "Longer query than target",
			query:    "main.go.test",
			target:   "main.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSubsequence(tt.query, tt.target)
			if got != tt.expected {
				t.Errorf("IsSubsequence(%q, %q) = %v; want %v", tt.query, tt.target, got, tt.expected)
			}
		})
	}
}
