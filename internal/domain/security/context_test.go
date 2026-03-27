// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextApprovedTools(t *testing.T) {
	ctx := context.Background()

	// 1. Initial state: no current tool, no approved tools
	assert.False(t, IsCurrentToolApproved(ctx), "Initial context should not be approved")

	// 2. Set current tool but no approved tools
	ctxWithTool := WithCurrentTool(ctx, "tool1")
	assert.False(t, IsCurrentToolApproved(ctxWithTool), "Tool should not be approved if not in approved list")

	// 3. Set approved tools but no current tool
	ctxWithApproved := WithApprovedTools(ctx, []string{"tool1", "tool2"})
	assert.False(t, IsCurrentToolApproved(ctxWithApproved), "Should not be approved if no current tool is set")

	// 4. Set both: tool1 is approved
	ctxBoth := WithCurrentTool(ctxWithApproved, "tool1")
	assert.True(t, IsCurrentToolApproved(ctxBoth), "tool1 should be approved")

	// 5. Set both: tool3 is NOT approved
	ctxBothOther := WithCurrentTool(ctxWithApproved, "tool3")
	assert.False(t, IsCurrentToolApproved(ctxBothOther), "tool3 should not be approved")

	// 6. Verify original context is unchanged
	assert.False(t, IsCurrentToolApproved(ctx), "Original context should remain unchanged")
}
