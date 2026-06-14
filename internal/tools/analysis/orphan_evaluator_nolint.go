// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

// nolintGateEvaluator checks whether a symbol is annotated with
// //nolint:deadcode and, if so, excludes it from orphan findings.
// This is step 1 of the original evaluateOrphan pipeline.
type nolintGateEvaluator struct {
	analyzer *defaultDeadCodeAnalyzer
}

// evaluate implements orphanEvaluator. If the symbol has a
// //nolint:deadcode directive, nil is returned to exclude it
// from findings. Otherwise, the context is passed through unchanged.
func (e *nolintGateEvaluator) evaluate(ctx *orphanEvalContext) *orphanEvalContext {
	if ctx == nil {
		return nil
	}
	if ctx.meta == nil || ctx.state == nil {
		return ctx
	}
	if ctx.meta.obj == nil {
		return ctx
	}
	if isNolintDeadcode(ctx.meta.obj, ctx.state) {
		return nil
	}
	return ctx
}
