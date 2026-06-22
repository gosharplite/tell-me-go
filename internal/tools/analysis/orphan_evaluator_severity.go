// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

// severityClassifierEvaluator classifies an orphan symbol as DEAD or
// PRIVATE based on usage counts and populates the initial Reason.
// This is step 3 of the original evaluateOrphan pipeline.
type severityClassifierEvaluator struct{}

func (e *severityClassifierEvaluator) evaluate(ctx *orphanEvalContext) *orphanEvalContext {
	if ctx == nil {
		return nil
	}
	if ctx.state == nil || ctx.meta == nil {
		return nil
	}
	total := ctx.state.totalUses[ctx.id]

	var severity, reason string
	if total == 0 {
		severity = "DEAD"
		reason = "No references found within the module (including interfaces/tests)."
	} else {
		severity = "PRIVATE"
		reason = "Exported symbol is only used within its own package."
	}

	ctx.report = &OrphanReport{
		Symbol:     ctx.displayName,
		Pkg:        ctx.meta.pkgPath,
		Type:       ctx.meta.symType,
		Severity:   severity,
		Reason:     reason,
		Complexity: ctx.complexity,
		Impact:     ctx.impact,
	}
	return ctx
}
