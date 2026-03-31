// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build linux

package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxMetricsProvider(t *testing.T) {
	// Setup mock filesystem
	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")
	err := os.Mkdir(procDir, 0755)
	if err != nil {
		t.Fatalf("failed to create proc dir: %v", err)
	}

	// Override procRoot
	originalRoot := procRoot
	procRoot = tmpDir + "/"
	defer func() { procRoot = originalRoot }()

	p := &LinuxMetricsProvider{}

	t.Run("NewSystemMetricsProvider", func(t *testing.T) {
		provider := NewSystemMetricsProvider()
		if _, ok := provider.(*LinuxMetricsProvider); !ok {
			t.Errorf("NewSystemMetricsProvider() did not return a *LinuxMetricsProvider")
		}
	})

	t.Run("Happy Path: GetCPUStats", func(t *testing.T) {
		// Mock /proc/stat
		// cpu 100 200 300 400 ...
		// index 1: user, 2: nice, 3: system, 4: idle
		statContent := "cpu 100 200 300 400 50 10 5 0 0 0\n"
		err := os.WriteFile(filepath.Join(procDir, "stat"), []byte(statContent), 0644)
		if err != nil {
			t.Fatalf("failed to write mock stat: %v", err)
		}

		total, idle := p.GetCPUStats()
		expectedTotal := int64(100 + 200 + 300 + 400 + 50 + 10 + 5)
		expectedIdle := int64(400)

		if total != expectedTotal {
			t.Errorf("GetCPUStats() total = %d, want %d", total, expectedTotal)
		}
		if idle != expectedIdle {
			t.Errorf("GetCPUStats() idle = %d, want %d", idle, expectedIdle)
		}
	})

	t.Run("Happy Path: GetMemoryPercent", func(t *testing.T) {
		// Mock /proc/meminfo
		meminfoContent := "MemTotal: 1000 kB\nMemAvailable: 250 kB\n"
		err := os.WriteFile(filepath.Join(procDir, "meminfo"), []byte(meminfoContent), 0644)
		if err != nil {
			t.Fatalf("failed to write mock meminfo: %v", err)
		}

		percent := p.GetMemoryPercent()
		// 100 * (1 - (250/1000)) = 100 * (1 - 0.25) = 75.0
		expected := 75.0

		if percent != expected {
			t.Errorf("GetMemoryPercent() = %f, want %f", percent, expected)
		}
	})

	t.Run("Edge Case: Missing Files", func(t *testing.T) {
		_ = os.Remove(filepath.Join(procDir, "stat"))
		_ = os.Remove(filepath.Join(procDir, "meminfo"))

		// GetCPUStats should fallback to runtime/metrics which returns 0 in tests usually or at least doesn't panic
		_, idle := p.GetCPUStats()
		// We don't assert 0 for total since it might return runtime metrics, but it shouldn't panic.
		// If idle is 0, it means the /proc/stat read failed and it fell back.
		if idle != 0 {
			t.Errorf("expected idle 0 for missing file, got %d", idle)
		}

		percent := p.GetMemoryPercent()
		if percent != 0.0 {
			t.Errorf("expected 0.0 for missing meminfo, got %f", percent)
		}
	})

	t.Run("Edge Case: Corrupted Format", func(t *testing.T) {
		err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("cpu not-a-number\n"), 0644)
		if err != nil {
			t.Fatalf("failed to write mock stat: %v", err)
		}
		total, idle := p.GetCPUStats()
		if total != 0 || idle != 0 {
			t.Logf("Fallback or partial read detected during corrupted format test: total=%d, idle=%d", total, idle)
		}

		err = os.WriteFile(filepath.Join(procDir, "meminfo"), []byte("MemTotal: invalid\nMemAvailable: 0\n"), 0644)
		if err != nil {
			t.Fatalf("failed to write mock meminfo: %v", err)
		}
		percent := p.GetMemoryPercent()
		if percent != 0.0 {
			t.Errorf("expected 0.0 for invalid meminfo, got %f", percent)
		}
	})

	t.Run("Edge Case: Empty Files", func(t *testing.T) {
		err := os.WriteFile(filepath.Join(procDir, "stat"), []byte(""), 0644)
		if err != nil {
			t.Fatalf("failed to write mock stat: %v", err)
		}
		p.GetCPUStats() // No panic

		err = os.WriteFile(filepath.Join(procDir, "meminfo"), []byte(""), 0644)
		if err != nil {
			t.Fatalf("failed to write mock meminfo: %v", err)
		}
		p.GetMemoryPercent() // No panic
	})
}
