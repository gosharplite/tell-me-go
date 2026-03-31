// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build linux

package telemetry

import (
	"bufio"
	"os"
	"runtime/metrics"
	"strconv"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

var procRoot = "/"

type LinuxMetricsProvider struct{}

func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &LinuxMetricsProvider{}
}

func (p *LinuxMetricsProvider) GetCPUStats() (int64, int64) {
	// Try to get host CPU stats from /proc/stat first for better visibility of tool execution
	f, err := os.Open(procRoot + "proc/stat")
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
	const cpuMetric = "/cpu/classes/total:cpu-seconds"
	samples := make([]metrics.Sample, 1)
	samples[0].Name = cpuMetric
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindFloat64 {
		return int64(samples[0].Value.Float64() * 1e9), 0
	}
	return 0, 0
}

func (p *LinuxMetricsProvider) GetMemoryPercent() float64 {
	f, err := os.Open(procRoot + "proc/meminfo")
	if err != nil {
		return 0.0
	}
	defer func() { _ = f.Close() }()

	var memTotal, memAvailable int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memTotal, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memAvailable, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		}
		if memTotal > 0 && memAvailable > 0 {
			break
		}
	}

	if memTotal == 0 {
		return 0.0
	}
	return 100.0 * (1.0 - (float64(memAvailable) / float64(memTotal)))
}
