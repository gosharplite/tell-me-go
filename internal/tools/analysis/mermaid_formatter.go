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

type formatState struct {
	inLoop bool
}

// Format transforms a slice of CallFrame into a Mermaid sequence diagram string.
func (f *MermaidFormatter) Format(frames []CallFrame) string {
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

func (f *MermaidFormatter) writeParticipants(sb *strings.Builder, frames []CallFrame) {
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
		sb.WriteString(fmt.Sprintf("    participant %s as %s\n", sanitize(p), p))
	}
}

func (f *MermaidFormatter) renderFrame(sb *strings.Builder, frame CallFrame, state *formatState) {
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
		sb.WriteString(fmt.Sprintf("    %s->>%s: %s (async)\n", from, to, frame.Function))
	} else {
		sb.WriteString(fmt.Sprintf("    %s->>+%s: %s\n", from, to, frame.Function))
		ret := frame.Return
		if ret == "" {
			ret = " "
		}
		sb.WriteString(fmt.Sprintf("    %s-->>-%s: %s\n", to, from, ret))
	}
}
