// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

// textMatchWarningEvaluator appends a hedge warning when a raw text
// search finds the symbol name in source files outside its declaring
// package. This warning is suppressed when --deep mode is active and
// the symbol is a method (deepVerificationEvaluator already handled
// that case with type-aware verification).
//
// This is step 6 of the original evaluateOrphan pipeline.
type textMatchWarningEvaluator struct {
	analyzer *defaultDeadCodeAnalyzer
}

func (e *textMatchWarningEvaluator) evaluate(ctx *orphanEvalContext) *orphanEvalContext {
	if ctx == nil || ctx.report == nil {
		return ctx
	}
	if ctx.meta == nil || ctx.state == nil {
		return ctx
	}
	if e.analyzer == nil {
		return ctx
	}
	// Suppressed when deep verification already handled this method.
	if ctx.deep && ctx.meta.isMethod {
		return ctx
	}
	if e.analyzer.hasTextMatchOutsidePackage(ctx.state, ctx.meta.name, ctx.meta.pkgPath) {
		ctx.report.Reason += " [WARNING: Text search found potential cross-package usage. Verify this is not a false positive due to structural typing.]"
	}
	return ctx
}
