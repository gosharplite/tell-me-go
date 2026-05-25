// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

// complexityReclassifierEvaluator rewrites the Reason field when a
// PRIVATE-severity orphan has high cyclomatic complexity (≥10).
// This is step 4 of the original evaluateOrphan pipeline.
type complexityReclassifierEvaluator struct{}

func (e *complexityReclassifierEvaluator) evaluate(ctx *orphanEvalContext) *orphanEvalContext {
	if ctx == nil || ctx.report == nil {
		return ctx
	}
	if ctx.report.Severity == "PRIVATE" && ctx.complexity >= 10 {
		ctx.report.Reason = "High Priority Refactoring Candidate: can be refactored with zero external impact."
	}
	return ctx
}
