// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build linux

package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

type linuxMetricsTestCase struct {
	name           string
	createStat     bool
	procStat       string
	createMeminfo  bool
	procMeminfo    string
	wantCPUTotal   int64
	wantCPUIdle    int64
	wantMemPercent float64
	checkCPU       bool
}

func TestLinuxMetricsProvider(t *testing.T) {
	t.Run("NewSystemMetricsProvider_ReturnsLinuxProvider", func(t *testing.T) {
		provider := NewSystemMetricsProvider()
		if _, ok := provider.(*linuxMetricsProvider); !ok {
			t.Errorf("NewSystemMetricsProvider() did not return a *linuxMetricsProvider")
		}
	})

	tests := []linuxMetricsTestCase{
		{
			name:           "valid_proc_stat_and_meminfo_parses_correctly",
			createStat:     true,
			procStat:       "cpu 100 200 300 400 50 10 5 0 0 0\n",
			createMeminfo:  true,
			procMeminfo:    "MemTotal: 1000 kB\nMemAvailable: 250 kB\n",
			wantCPUTotal:   1065, // 100+200+300+400+50+10+5
			wantCPUIdle:    400,
			wantMemPercent: 75.0,
			checkCPU:       true,
		},
		{
			name:           "missing_proc_files_falls_back_gracefully",
			createStat:     false,
			createMeminfo:  false,
			wantMemPercent: 0.0,
			checkCPU:       false,
		},
		{
			name:           "corrupted_stat_format_returns_zero_or_fallback",
			createStat:     true,
			procStat:       "cpu not-a-number\n",
			createMeminfo:  true,
			procMeminfo:    "MemTotal: 1000 kB\nMemAvailable: 250 kB\n",
			wantMemPercent: 75.0,
			checkCPU:       false,
		},
		{
			name:           "corrupted_meminfo_format_returns_zero",
			createStat:     true,
			procStat:       "cpu 100 200 300 400 50 10 5 0 0 0\n",
			createMeminfo:  true,
			procMeminfo:    "MemTotal: invalid\nMemAvailable: 0\n",
			wantCPUTotal:   1065,
			wantCPUIdle:    400,
			wantMemPercent: 0.0,
			checkCPU:       true,
		},
		{
			name:           "empty_proc_files_do_not_panic",
			createStat:     true,
			procStat:       "",
			createMeminfo:  true,
			procMeminfo:    "",
			wantMemPercent: 0.0,
			checkCPU:       false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runLinuxMetricsTest(t, tt)
		})
	}
}

func runLinuxMetricsTest(t *testing.T, tt linuxMetricsTestCase) {
	t.Helper()

	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")
	if err := os.Mkdir(procDir, 0755); err != nil {
		t.Fatalf("failed to create proc dir: %v", err)
	}

	setupMockFiles(t, procDir, tt)

	p := &linuxMetricsProvider{root: tmpDir}

	verifyCPUStats(t, p, tt)
	verifyMemoryPercent(t, p, tt)
}

func setupMockFiles(t *testing.T, procDir string, tt linuxMetricsTestCase) {
	t.Helper()
	if tt.createStat {
		if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte(tt.procStat), 0644); err != nil {
			t.Fatalf("failed to write mock stat: %v", err)
		}
	}
	if tt.createMeminfo {
		if err := os.WriteFile(filepath.Join(procDir, "meminfo"), []byte(tt.procMeminfo), 0644); err != nil {
			t.Fatalf("failed to write mock meminfo: %v", err)
		}
	}
}

func verifyCPUStats(t *testing.T, p *linuxMetricsProvider, tt linuxMetricsTestCase) {
	t.Helper()
	total, idle := p.GetCPUStats()
	if tt.checkCPU {
		if total != tt.wantCPUTotal {
			t.Errorf("GetCPUStats() total = %d, want %d", total, tt.wantCPUTotal)
		}
		if idle != tt.wantCPUIdle {
			t.Errorf("GetCPUStats() idle = %d, want %d", idle, tt.wantCPUIdle)
		}
	}
}

func verifyMemoryPercent(t *testing.T, p *linuxMetricsProvider, tt linuxMetricsTestCase) {
	t.Helper()
	mem := p.GetMemoryPercent()
	if mem != tt.wantMemPercent {
		t.Errorf("GetMemoryPercent() = %f, want %f", mem, tt.wantMemPercent)
	}
}
