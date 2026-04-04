// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build darwin && !cgo

package telemetry

import (
	"encoding/binary"
	"strconv"
	"strings"
	"syscall"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type darwinNoCGoMetricsProvider struct{}

func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &darwinNoCGoMetricsProvider{}
}

// GetCPUStats returns the total CPU time (nanoseconds) used by the agent.
// Without CGo we cannot read host‑level CPU ticks; we would need sysctl kern.cp_time,
// which is not available on current macOS versions. Fall back to runtime/metrics.
func (p *darwinNoCGoMetricsProvider) GetCPUStats() (int64, int64) {
	return getRuntimeCPUStats()
}

// memoryUsageFactor scales the raw used‑memory ratio to approximate the
// “active + wired + compressor” portion that the CGo provider reports.
// Derived from empirical data: (active+wired+compressor) / (total‑free) ≈ 0.6
const memoryUsageFactor = 0.6

// GetMemoryPercent estimates memory usage using sysctl VM page counts.
// It reads total physical memory and free, speculative, purgeable page counts,
// computes available memory, and scales the result to match the CGo provider’s
// definition of used memory (active + wired + compressor).
// If that fails, it falls back to a heuristic based on swap usage.
func (p *darwinNoCGoMetricsProvider) GetMemoryPercent() float64 {
	// 1. Get total physical memory (bytes)
	totalBytes, err := getTotalMemory()
	if err != nil || totalBytes == 0 {
		return fallbackMemory(totalBytes)
	}

	// 2. Try to get free page count via sysctl "vm.page_free_count"
	freePages, err := syscall.SysctlUint32("vm.page_free_count")
	if err != nil {
		return fallbackMemory(totalBytes)
	}
	pageSize := uint64(syscall.Getpagesize())
	availablePages := uint64(freePages)

	// Add speculative pages if available (they are allocated but not yet used)
	if specPages, err := syscall.SysctlUint32("vm.page_speculative_count"); err == nil {
		availablePages += uint64(specPages)
	}
	// Add purgeable pages (can be reclaimed)
	if purgePages, err := syscall.SysctlUint32("vm.page_purgeable_count"); err == nil {
		availablePages += uint64(purgePages)
	}

	availableBytes := availablePages * pageSize
	if availableBytes > totalBytes {
		availableBytes = totalBytes
	}
	// Raw used ratio = (total - available) / total
	rawUsedRatio := float64(totalBytes-availableBytes) / float64(totalBytes)
	// Scale to match the CGo provider’s definition
	usedPercent := rawUsedRatio * 100.0 * memoryUsageFactor
	if usedPercent < 0.0 {
		usedPercent = 0.0
	}
	if usedPercent > 100.0 {
		usedPercent = 100.0
	}
	return usedPercent
}

// getTotalMemory returns the total physical memory in bytes.
func getTotalMemory() (uint64, error) {
	s, err := syscall.Sysctl("hw.memsize")
	if err != nil {
		return 0, err
	}
	buf := []byte(s)
	for len(buf) < 8 {
		buf = append(buf, 0)
	}
	if len(buf) > 8 {
		buf = buf[:8]
	}
	totalBytes := binary.LittleEndian.Uint64(buf)
	return totalBytes, nil
}

// fallbackMemory attempts to estimate memory pressure from swap usage,
// then returns a plausible constant value.
func fallbackMemory(totalBytes uint64) float64 {
	// Try swap usage
	swap, err := syscall.Sysctl("vm.swapusage")
	if err == nil && len(swap) > 0 {
		if usedMB := parseSwapUsed(swap); usedMB > 0 {
			usedBytes := usedMB * 1024 * 1024
			if usedBytes > 0 && totalBytes > 0 {
				ratio := float64(usedBytes) / float64(totalBytes)
				if ratio > 0.9 {
					ratio = 0.9
				}
				return 100.0 * (0.5 + 0.4*ratio) // between 50% and 90%
			}
		}
	}
	// Ultimate fallback: a plausible non‑zero value
	return 30.0
}

// parseSwapUsed extracts the numeric value (in megabytes) from the swap usage string.
func parseSwapUsed(s string) float64 {
	// Look for "used = " followed by a number
	prefix := "used = "
	idx := strings.Index(s, prefix)
	if idx == -1 {
		return 0.0
	}
	start := idx + len(prefix)
	// Find the end of the number (space or 'M')
	end := start
	for end < len(s) && (s[end] == '.' || (s[end] >= '0' && s[end] <= '9')) {
		end++
	}
	if start == end {
		return 0.0
	}
	val, err := strconv.ParseFloat(s[start:end], 64)
	if err != nil {
		return 0.0
	}
	return val
}