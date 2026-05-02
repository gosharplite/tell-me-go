// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

// PipelineFormatter formats raw ADO domain types into human-readable strings
// for LLM/CLI consumption.
//
// Per ADR docs/adr/2026-05-ado-pipeline-formatter.md, the ADO infrastructure
// adapter (AdoManager) MUST return raw domain structs. Presentation belongs to
// the consumer (tool registration layer), which holds a PipelineFormatter and
// wraps results into tools.ToolResult at the registration boundary.
//
// This interface is intentionally defined where it is consumed (the ado
// package's registration code) following the Go idiom "accept interfaces,
// return structs".
type PipelineFormatter interface {
	FormatBranchRef(branch string) string
	FormatPipelineRunsList(pipelineID int, runs []adoPipelineRun) string
	FormatPipelineRunDetail(run *adoPipelineRunDetail) string
}

// pipelineFormatter is the default PipelineFormatter implementation.
// It is unexported because callers should depend on the interface, not the
// concrete type.
type pipelineFormatter struct{}

// NewPipelineFormatter returns the default PipelineFormatter implementation.
// Exported so the registration layer (and tests) can construct one.
func NewPipelineFormatter() PipelineFormatter {
	return &pipelineFormatter{}
}
