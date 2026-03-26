// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package trie

import (
	"sort"
)

// Node represents a single character in the Trie.
type Node struct {
	children map[rune]*Node
	isEnd    bool
	word     string // Store the full word at the end node for faster retrieval
}

// Trie is a thread-safe prefix tree for word suggestions.
// Optimized for read-heavy operations.
type Trie struct {
	root *Node
}

// NewTrie creates an empty Trie.
func NewTrie() *Trie {
	return &Trie{
		root: &Node{
			children: make(map[rune]*Node),
		},
	}
}

// Insert adds a word to the Trie.
func (t *Trie) Insert(word string) {
	if word == "" {
		return
	}
	current := t.root
	for _, char := range word {
		if _, ok := current.children[char]; !ok {
			current.children[char] = &Node{
				children: make(map[rune]*Node),
			}
		}
		current = current.children[char]
	}
	current.isEnd = true
	current.word = word
}

// SearchPrefix finds up to limit words starting with the given prefix.
func (t *Trie) SearchPrefix(prefix string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	current := t.root
	for _, char := range prefix {
		if _, ok := current.children[char]; !ok {
			return nil
		}
		current = current.children[char]
	}

	var results []string
	t.collect(current, &results, limit)
	return results
}

func (t *Trie) collect(node *Node, results *[]string, limit int) {
	if len(*results) >= limit {
		return
	}

	if node.isEnd {
		*results = append(*results, node.word)
	}

	if len(*results) >= limit {
		return
	}

	// To ensure deterministic results and potentially better suggestions (alphabetical),
	// we sort the keys of children.
	keys := make([]rune, 0, len(node.children))
	for k := range node.children {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	for _, k := range keys {
		t.collect(node.children[k], results, limit)
		if len(*results) >= limit {
			return
		}
	}
}

// Clear removes all words from the Trie.
func (t *Trie) Clear() {
	t.root = &Node{
		children: make(map[rune]*Node),
	}
}
