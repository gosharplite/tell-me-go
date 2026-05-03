// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build linux

package telemetry

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type linuxMetricsProvider struct {
	// root is the base directory for system files (e.g., /proc).
	// Defaults to "/" if empty.
	root string
}

func (p *linuxMetricsProvider) getRoot() string {
	if p.root != "" {
		return p.root
	}
	return "/"
}

func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &linuxMetricsProvider{}
}

func (p *linuxMetricsProvider) GetCPUStats() (int64, int64) {
	// Try to get host CPU stats from /proc/stat first for better visibility of tool execution
	f, err := os.Open(filepath.Join(p.getRoot(), "proc", "stat"))
	if err == nil {
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		if scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "cpu ") {
				fields := strings.Fields(line)
				if len(fields) >= 5 {
					// Format: user nice system idle iowait irq softirq steal guest guest_nice
					var total, idle int64
					for i := 1; i < len(fields); i++ {
						val, _ := strconv.ParseInt(fields[i], 10, 64)
						total += val
						if i == 4 { // idle index is 4 (starts at 1)
							idle = val
						}
					}
					return total, idle
				}
			}
		}
	}

	// Fallback: Use runtime/metrics for total CPU time (nanoseconds)
	return getRuntimeCPUStats()
}

// parseMeminfoLine extracts memory values from a single /proc/meminfo line.
// Returns true when both MemTotal and MemAvailable have been found.
func parseMeminfoLine(line string, memTotal, memAvailable *int64) bool {
	if strings.HasPrefix(line, "MemTotal:") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			*memTotal, _ = strconv.ParseInt(fields[1], 10, 64)
		}
	} else if strings.HasPrefix(line, "MemAvailable:") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			*memAvailable, _ = strconv.ParseInt(fields[1], 10, 64)
		}
	}

	return *memTotal > 0 && *memAvailable > 0
}

func (p *linuxMetricsProvider) GetMemoryPercent() float64 {
	f, err := os.Open(filepath.Join(p.getRoot(), "proc", "meminfo"))
	if err != nil {
		return 0.0
	}
	defer func() { _ = f.Close() }()

	var memTotal, memAvailable int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if parseMeminfoLine(scanner.Text(), &memTotal, &memAvailable) {
			break
		}
	}

	if memTotal == 0 {
		return 0.0
	}
	return 100.0 * (1.0 - (float64(memAvailable) / float64(memTotal)))
}
