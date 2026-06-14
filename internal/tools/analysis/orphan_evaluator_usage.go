// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

// usageGateEvaluator checks whether a symbol has any external usage.
// If total > 0 AND external > 0, the symbol is alive cross-package
// and is excluded from orphan findings.
// This is step 2 of the original evaluateOrphan pipeline.
type usageGateEvaluator struct{}

// evaluate implements orphanEvaluator. If the symbol has both total
// and external references > 0, nil is returned to exclude it.
func (e *usageGateEvaluator) evaluate(ctx *orphanEvalContext) *orphanEvalContext {
	if ctx == nil {
		return ctx
	}
	if ctx.state == nil {
		return ctx
	}
	total := ctx.state.totalUses[ctx.id]
	external := ctx.state.externalUses[ctx.id]
	if total > 0 && external > 0 {
		return nil
	}
	return ctx
}
