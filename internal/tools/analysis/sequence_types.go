// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

// callFrame represents a single step in a sequence diagram.
type callFrame struct {
	From     string
	To       string
	Function string
	Async    bool
	InLoop   bool
	Return   string
}
