// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"strings"
)

// mermaidFormatter handles the conversion of call frames to Mermaid.js sequence diagrams.
type mermaidFormatter struct{}

// newMermaidFormatter creates a new mermaidFormatter.
func newMermaidFormatter() *mermaidFormatter {
	return &mermaidFormatter{}
}

type formatState struct {
	inLoop bool
}

// Format transforms a slice of callFrame into a Mermaid sequence diagram string.
func (f *mermaidFormatter) Format(frames []callFrame) string {
	// Empty frames produce a minimal valid sequenceDiagram with
	// no participants or interactions. The zero-value formatState
	// (inLoop=false) ensures no unbalanced loop-end is emitted.
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")

	f.writeParticipants(&b, frames)

	state := &formatState{}
	for _, frame := range frames {
		f.renderFrame(&b, frame, state)
	}

	if state.inLoop {
		b.WriteString("    end\n")
	}
	return b.String()
}

func (f *mermaidFormatter) writeParticipants(sb *strings.Builder, frames []callFrame) {
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
		_, _ = fmt.Fprintf(sb, "    participant %s as %s\n", sanitize(p), p)
	}
}

func (f *mermaidFormatter) renderFrame(sb *strings.Builder, frame callFrame, state *formatState) {
	if frame.InLoop && !state.inLoop {
		sb.WriteString("    loop for each\n")
		state.inLoop = true
	} else if !frame.InLoop && state.inLoop {
		sb.WriteString("    end\n")
		state.inLoop = false
	}

	from := sanitize(frame.From)
	to := sanitize(frame.To)

	if frame.Async {
		_, _ = fmt.Fprintf(sb, "    %s->>%s: %s (async)\n", from, to, frame.Function)
	} else {
		_, _ = fmt.Fprintf(sb, "    %s->>+%s: %s\n", from, to, frame.Function)
		ret := frame.Return
		if ret == "" {
			ret = " "
		}
		_, _ = fmt.Fprintf(sb, "    %s-->>-%s: %s\n", to, from, ret)
	}
}
