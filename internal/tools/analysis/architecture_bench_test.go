// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
)

func BenchmarkVerifyArchitecture_Baseline(b *testing.B) {
	ctx := context.Background()
	sm := &mockSecurityProvider{} // Allows "go" command by default in mockSecurityProvider
	executor := &exec.RealExecutor{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := &architectureManager{
			SP:   sm,
			Exec: executor,
		}
		_, err := m.VerifyArchitecture(ctx, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyArchitecture_Optimized(b *testing.B) {
	ctx := context.Background()
	sm := &mockSecurityProvider{}
	idx, err := newIndexer(".")
	if err != nil {
		b.Fatal(err)
	}
	if err := idx.Refresh(ctx); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := &architectureManager{
			SP:  sm,
			idx: idx,
		}
		_, err := m.VerifyArchitecture(ctx, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}
