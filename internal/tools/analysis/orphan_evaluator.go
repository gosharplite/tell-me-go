// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

// orphanEvalContext carries the full context for a single orphan evaluation step.
// Read-only fields: id, meta, state, deep, complexity, impact, displayName.
// Mutable field: report — the *orphanReport being built; nil means "exclude this symbol."
type orphanEvalContext struct {
	id          string
	meta        *symMeta
	state       *scanState
	deep        bool
	complexity  int
	impact      int
	displayName string
	report      *orphanReport
}

// orphanEvaluator is a single step in the orphan classification pipeline.
// It receives an eval context and returns the (possibly modified) context.
// If ctx.report is nil after evaluate returns, the pipeline stops and the
// symbol is excluded from orphan findings.
type orphanEvaluator interface {
	evaluate(ctx *orphanEvalContext) *orphanEvalContext
}
