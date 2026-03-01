// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package execution

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestService_NilExecutor(t *testing.T) {
	s := &Service{}
	
	_, err := s.Execute(context.Background(), &llm.Content{}, 1, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}
