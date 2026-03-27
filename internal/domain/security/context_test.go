// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextApproval(t *testing.T) {
	ctx := context.Background()

	// 1. Initial state: not approved
	assert.False(t, IsApproved(ctx), "Initial context should not be approved")

	// 2. Wrap with approval
	approvedCtx := WithApproval(ctx, true)
	assert.True(t, IsApproved(approvedCtx), "Context with approval should be approved")

	// 3. Wrap with disapproval
	deniedCtx := WithApproval(ctx, false)
	assert.False(t, IsApproved(deniedCtx), "Context with false approval should not be approved")

	// 4. Verify original context is unchanged
	assert.False(t, IsApproved(ctx), "Original context should remain unchanged")
}
