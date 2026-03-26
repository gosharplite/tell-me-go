// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package trie

import (
	"reflect"
	"testing"
)

func TestTrie(t *testing.T) {
	tr := NewTrie()

	words := []string{"apple", "apply", "application", "banana", "band", "bandwidth"}
	for _, w := range words {
		tr.Insert(w)
	}

	tests := []struct {
		name     string
		prefix   string
		limit    int
		expected []string
	}{
		{
			name:     "apple prefix",
			prefix:   "apple",
			limit:    5,
			expected: []string{"apple"},
		},
		{
			name:     "app prefix",
			prefix:   "app",
			limit:    2,
			expected: []string{"apple", "application"}, // e < i
		},
		{
			name:     "ban prefix limit 2",
			prefix:   "ban",
			limit:    2,
			expected: []string{"banana", "band"},
		},
		{
			name:     "empty prefix",
			prefix:   "",
			limit:    2,
			expected: []string{"apple", "application"},
		},
		{
			name:     "non-existent prefix",
			prefix:   "cat",
			limit:    5,
			expected: nil,
		},
		{
			name:     "limit zero",
			prefix:   "app",
			limit:    0,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tr.SearchPrefix(tt.prefix, tt.limit)
			// Sort results for comparison if order is not strictly alphabetical (our collect method uses sorted keys)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("SearchPrefix(%q, %d) = %v; want %v", tt.prefix, tt.limit, got, tt.expected)
			}
		})
	}
}

func TestTrieOrder(t *testing.T) {
	tr := NewTrie()
	words := []string{"app", "apple", "apply", "application"}
	for _, w := range words {
		tr.Insert(w)
	}

	// Alphabetical collection:
	// app
	// application
	// apple
	// apply
	// Root children 'a' -> 'p' -> 'p' (isEnd: app) -> 'l' or 'i'
	// 'i' comes before 'l', so application before apple
	got := tr.SearchPrefix("app", 10)
	expected := []string{"app", "apple", "application", "apply"}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v; want %v", got, expected)
	}
}
