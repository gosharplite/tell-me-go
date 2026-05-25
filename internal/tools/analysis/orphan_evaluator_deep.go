// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

// deepVerificationEvaluator performs type-aware AST verification to
// confirm whether a method has cross-package callers. This only runs
// when --deep mode is active.
//
// If a cross-package caller is confirmed, ctx.report is set to nil
// (symbol is alive). Otherwise, deepVerifiedDead is appended to the
// Reason field, and the text-search warning is suppressed (handled
// by omitting textMatchWarningEvaluator from the pipeline).
//
// This is step 5 of the original evaluateOrphan pipeline.
type deepVerificationEvaluator struct {
	analyzer *defaultDeadCodeAnalyzer
}

func (e *deepVerificationEvaluator) evaluate(ctx *orphanEvalContext) *orphanEvalContext {
	if ctx == nil || ctx.report == nil {
		return ctx
	}
	if !ctx.deep || !ctx.meta.isMethod {
		return ctx
	}

	if e.analyzer.resolveCrossPackageMethodUsages(ctx.state, ctx.meta.name, ctx.id, ctx.meta.pkgPath) {
		return nil
	}

	ctx.report.Reason += deepVerifiedDead
	return ctx
}
