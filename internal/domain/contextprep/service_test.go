// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package contextprep

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestService_NilCM(t *testing.T) {
	s := &service{}
	
	_, err := s.Prepare(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")

	err = s.AddContent(context.Background(), &llm.Content{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}
