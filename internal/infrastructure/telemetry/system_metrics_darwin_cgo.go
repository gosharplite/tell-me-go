// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build darwin && cgo

package telemetry

/*
#include <mach/mach.h>
#include <mach/mach_host.h>
*/
import "C"

import (
	"log/slog"
	"unsafe"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"golang.org/x/sys/unix"
)

type darwinMetricsProvider struct{}

// NewSystemMetricsProvider returns the macOS‑specific implementation of SystemMetricsProvider.
func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &darwinMetricsProvider{}
}

// GetCPUStats returns the total and idle CPU ticks for the entire host.
// It uses the Mach host_statistics64 API with HOST_CPU_LOAD_INFO.
func (p *darwinMetricsProvider) GetCPUStats() (int64, int64) {
	// Use Mach host_statistics64 with HOST_CPU_LOAD_INFO
	var cpuInfo C.host_cpu_load_info_data_t
	var count C.mach_msg_type_number_t = C.HOST_CPU_LOAD_INFO_COUNT
	host := C.mach_host_self()
	ret := C.host_statistics64(host, C.HOST_CPU_LOAD_INFO, (C.host_info64_t)(unsafe.Pointer(&cpuInfo)), &count)
	if ret != C.KERN_SUCCESS {
		// Fall back to the agent‑only runtime/metrics if Mach API fails
		total, err := getRuntimeCPUStatsFn()
		if err != nil {
			slog.Debug("getRuntimeCPUStats failed, falling back to zero", "err", err)
			return 0, 0
		}
		return total, 0
	}

	// cpuInfo.cpu_ticks is an array of integer_t[CPU_STATE_MAX] where:
	// [CPU_STATE_USER]   = 0  (user ticks)
	// [CPU_STATE_SYSTEM] = 1  (system ticks)
	// [CPU_STATE_IDLE]   = 2  (idle ticks)
	// [CPU_STATE_NICE]   = 3  (nice ticks, usually zero on macOS)
	user := uint64(cpuInfo.cpu_ticks[C.CPU_STATE_USER])
	system := uint64(cpuInfo.cpu_ticks[C.CPU_STATE_SYSTEM])
	idle := uint64(cpuInfo.cpu_ticks[C.CPU_STATE_IDLE])
	nice := uint64(cpuInfo.cpu_ticks[C.CPU_STATE_NICE])
	total := user + nice + system + idle

	// Return as int64 (ticks)
	return int64(total), int64(idle)
}

// GetMemoryPercent returns the host memory‑usage percentage (0‑100) using sysctl.
func (p *darwinMetricsProvider) GetMemoryPercent() float64 {
	// 1. Get total physical memory via sysctl
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0.0
	}

	// 2. Get used memory via vm_statistics
	var vmStats C.vm_statistics64_data_t
	var count C.mach_msg_type_number_t = C.HOST_VM_INFO64_COUNT
	host := C.mach_host_self()
	ret := C.host_statistics64(host, C.HOST_VM_INFO64, (C.host_info64_t)(unsafe.Pointer(&vmStats)), &count)
	if ret != C.KERN_SUCCESS {
		return 0.0
	}

	// 3. Compute used memory (active + wired + compressed)
	pageSize := uint64(unix.Getpagesize())
	active := uint64(vmStats.active_count)
	wired := uint64(vmStats.wire_count)
	compressor := uint64(vmStats.compressor_page_count)
	used := (active + wired + compressor) * pageSize

	if total == 0 {
		return 0.0
	}
	return 100.0 * float64(used) / float64(total)
}
