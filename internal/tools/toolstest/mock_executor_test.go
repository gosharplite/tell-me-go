// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
)

func TestMockExecutor(t *testing.T) {
	t.Parallel()
	t.Run("Output", func(t *testing.T) {
		t.Parallel()
		m := &toolstest.MockExecutor{
			OutputBytes: []byte("output"),
			Error:       nil,
		}
		ctx := context.Background()
		out, err := m.Output(ctx, "ls", "-la")

		assert.NoError(t, err)
		assert.Equal(t, []byte("output"), out)
		assert.Equal(t, "ls", m.CommandName)
		assert.Equal(t, []string{"-la"}, m.CommandArgs)
	})

	t.Run("CombinedOutput", func(t *testing.T) {
		t.Parallel()
		m := &toolstest.MockExecutor{
			OutputBytes: []byte("combined"),
			Error:       errors.New("failed"),
		}
		ctx := context.Background()
		out, err := m.CombinedOutput(ctx, "grep", "foo")

		assert.Error(t, err)
		assert.Equal(t, []byte("combined"), out)
		assert.Equal(t, "grep", m.CommandName)
		assert.Equal(t, []string{"foo"}, m.CommandArgs)
	})
}
