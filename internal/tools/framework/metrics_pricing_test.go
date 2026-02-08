// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
)

func TestGetPricing_Simple(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	got := GetPricing(context.Background(), sm, "")
	assert.NotEmpty(t, got.UpdatedAt)
	assert.NotEmpty(t, got.Models)
}
