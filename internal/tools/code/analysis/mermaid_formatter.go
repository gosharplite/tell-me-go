// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"strings"
)

// MermaidFormatter handles the conversion of call frames to Mermaid.js sequence diagrams.
type MermaidFormatter struct{}

// NewMermaidFormatter creates a new MermaidFormatter.
func NewMermaidFormatter() *MermaidFormatter {
	return &MermaidFormatter{}
}

// Format transforms a slice of CallFrame into a Mermaid sequence diagram string.
func (f *MermaidFormatter) Format(frames []CallFrame) string {
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")

	participants := make(map[string]bool)
	var orderedParticipants []string
	for _, frame := range frames {
		if !participants[frame.From] {
			participants[frame.From] = true
			orderedParticipants = append(orderedParticipants, frame.From)
		}
		if !participants[frame.To] {
			participants[frame.To] = true
			orderedParticipants = append(orderedParticipants, frame.To)
		}
	}

	for _, p := range orderedParticipants {
		b.WriteString(fmt.Sprintf("    participant %s as %s\n", sanitize(p), p))
	}

	inLoop := false
	for _, frame := range frames {
		if frame.InLoop && !inLoop {
			b.WriteString("    loop for each\n")
			inLoop = true
		} else if !frame.InLoop && inLoop {
			b.WriteString("    end\n")
			inLoop = false
		}

		from := sanitize(frame.From)
		to := sanitize(frame.To)

		if frame.Async {
			b.WriteString(fmt.Sprintf("    %s->>%s: %s (async)\n", from, to, frame.Function))
		} else {
			b.WriteString(fmt.Sprintf("    %s->>+%s: %s\n", from, to, frame.Function))
			ret := frame.Return
			if ret == "" {
				ret = " "
			}
			b.WriteString(fmt.Sprintf("    %s-->>-%s: %s\n", to, from, ret))
		}
	}
	if inLoop {
		b.WriteString("    end\n")
	}
	return b.String()
}
