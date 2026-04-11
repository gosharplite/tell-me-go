// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package telemetry

import (
	"unsafe"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"golang.org/x/sys/windows"
)

var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes       = modkernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

// memoryStatusEx corresponds to the Windows MEMORYSTATUSEX structure.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

type windowsMetricsProvider struct{}

// NewSystemMetricsProvider returns the Windows-specific implementation of SystemMetricsProvider.
func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &windowsMetricsProvider{}
}

// GetCPUStats returns the total and idle CPU ticks for the entire host.
// It uses the GetSystemTimes Windows API via lazy loading.
func (p *windowsMetricsProvider) GetCPUStats() (int64, int64) {
	var idle, kernel, user windows.Filetime
	ret, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ret == 0 {
		// Fall back to the agent-only runtime/metrics if Windows API fails
		return getRuntimeCPUStats()
	}

	// Filetime is in 100-nanosecond intervals.
	// Total = Kernel + User (Kernel includes Idle on Windows)
	// Idle = Idle
	kernelTicks := int64(kernel.HighDateTime)<<32 | int64(kernel.LowDateTime)
	userTicks := int64(user.HighDateTime)<<32 | int64(user.LowDateTime)
	total := kernelTicks + userTicks
	idleTicks := int64(idle.HighDateTime)<<32 | int64(idle.LowDateTime)

	return total, idleTicks
}

// GetMemoryPercent returns the host memory-usage percentage (0-100) using GlobalMemoryStatusEx.
func (p *windowsMetricsProvider) GetMemoryPercent() float64 {
	var stat memoryStatusEx
	stat.length = uint32(unsafe.Sizeof(stat))
	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&stat)))
	if ret == 0 {
		return 0.0
	}

	if stat.totalPhys == 0 {
		return 0.0
	}

	// memoryLoad is the approximate percentage of physical memory that is in use (0-100).
	return float64(stat.memoryLoad)
}
