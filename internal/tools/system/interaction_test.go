// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package system

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestInteractionTool(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	it := NewInteractionTool(sm)
	ctx := context.Background()

	t.Run("Ask User", func(t *testing.T) {
		sm.SetInputReader(strings.NewReader("The answer is 42\n"))
		res, err := it.AskUser(ctx, map[string]interface{}{
			"question": "What is the answer?",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "The answer is 42") {
			t.Errorf("unexpected response: %s", res.Text)
		}
	})
}
