// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build darwin

package telemetry

/*
#include <mach/mach.h>
#include <mach/mach_host.h>
*/
import "C"

import (
	"encoding/binary"
	"syscall"
	"unsafe"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type darwinMetricsProvider struct{}

// NewSystemMetricsProvider returns the macOS‑specific implementation of SystemMetricsProvider.
func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &darwinMetricsProvider{}
}

// GetCPUStats returns the total CPU time (nanoseconds) used by the agent.
// It reuses the platform‑agnostic runtime/metrics fallback for now.
func (p *darwinMetricsProvider) GetCPUStats() (int64, int64) {
	return getRuntimeCPUStats()
}

// GetMemoryPercent returns the host memory‑usage percentage (0‑100) using sysctl.
func (p *darwinMetricsProvider) GetMemoryPercent() float64 {
	// 1. Get total physical memory via sysctl
	s, err := syscall.Sysctl("hw.memsize")
	if err != nil {
		return 0.0
	}
	buf := []byte(s)
	// sysctl strips trailing zero bytes; pad to 8 bytes (size of uint64)
	for len(buf) < 8 {
		buf = append(buf, 0)
	}
	if len(buf) > 8 {
		buf = buf[:8]
	}
	total := binary.LittleEndian.Uint64(buf)

	// 2. Get used memory via vm_statistics
	var vmStats C.vm_statistics64_data_t
	var count C.mach_msg_type_number_t = C.HOST_VM_INFO64_COUNT
	host := C.mach_host_self()
	ret := C.host_statistics64(host, C.HOST_VM_INFO64, (C.host_info64_t)(unsafe.Pointer(&vmStats)), &count)
	if ret != C.KERN_SUCCESS {
		return 0.0
	}

	// 3. Compute used memory (active + wired + compressed)
	pageSize := uint64(syscall.Getpagesize())
	active := uint64(vmStats.active_count)
	wired := uint64(vmStats.wire_count)
	compressor := uint64(vmStats.compressor_page_count)
	used := (active + wired + compressor) * pageSize

	if total == 0 {
		return 0.0
	}
	return 100.0 * float64(used) / float64(total)
}