// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
)

func BenchmarkAnalyzeSequenceFlow_Baseline(b *testing.B) {
	ctx := context.Background()
	sm := &mockSecurityProvider{}
	executor := &exec.RealExecutor{}
	idx, err := newIndexer(".")
	if err != nil {
		b.Fatal(err)
	}

	analyzer := newSequenceAnalyzer(executor, sm, idx)
	args := map[string]interface{}{
		"start_symbol": "github.com/gosharplite/tell-me-go/internal/agent.agent.Chat",
		"max_depth":    5.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := analyzer.AnalyzeSequenceFlow(ctx, args, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}
