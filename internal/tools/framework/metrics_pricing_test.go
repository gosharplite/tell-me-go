// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/stretchr/testify/assert"
)

func TestGetPricing_CacheLogic(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "global_prices.json")
	sm := security.NewSecurityManager(nil)

	// Sample data
	data := llm.PricingData{
		UpdatedAt: "2026-02-02T12:00:00Z",
		Models: map[string]llm.ModelPricing{
			"test-model": {Hit: 1.0, Miss: 2.0, Comp: 3.0},
		},
	}
	bytes, _ := json.Marshal(data)

	t.Run("Fresh Cache", func(t *testing.T) {
		_ = os.WriteFile(cachePath, bytes, 0644)
		// Set time to now
		_ = os.Chtimes(cachePath, time.Now(), time.Now())

		got := GetPricing(context.Background(), sm, tmpDir)
		assert.Equal(t, data.UpdatedAt, got.UpdatedAt)
		assert.Equal(t, 1.0, got.Models["test-model"].Hit)
	})

	t.Run("Stale Cache Returns Stale Data", func(t *testing.T) {
		_ = os.WriteFile(cachePath, bytes, 0644)
		// Set time to 2 days ago
		staleTime := time.Now().Add(-48 * time.Hour)
		_ = os.Chtimes(cachePath, staleTime, staleTime)

		got := GetPricing(context.Background(), sm, tmpDir)
		// Should return stale data immediately
		assert.Equal(t, data.UpdatedAt, got.UpdatedAt)

		// Small delay to allow background goroutine (though it might fail network, it shouldn't block)
		time.Sleep(10 * time.Millisecond)
	})

	t.Run("Missing Cache Returns Fallback or Remote", func(t *testing.T) {
		_ = os.Remove(cachePath)

		// Use a context that will timeout quickly for the remote fetch attempt
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		got := GetPricing(ctx, sm, tmpDir)
		// Should return either remote data or hardcoded fallback
		assert.NotEmpty(t, got.UpdatedAt)
		assert.NotEmpty(t, got.Models)
	})
}
