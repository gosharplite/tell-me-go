// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolchain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// BinaryInfo contains diagnostic information for a specific toolchain binary.
type BinaryInfo struct {
	Path       string `json:"path"`
	Version    string `json:"version_string,omitempty"`
	IsRequired bool   `json:"is_required"`
}

// ToolchainHealthChecker implements ports.HealthChecker for the system toolchain.
type ToolchainHealthChecker struct {
	exec     tools.CommandExecutor
	required []string
	optional []string
}

// NewToolchainHealthChecker creates a new ToolchainHealthChecker.
func NewToolchainHealthChecker(exec tools.CommandExecutor, required, optional []string) *ToolchainHealthChecker {
	return &ToolchainHealthChecker{
		exec:     exec,
		required: required,
		optional: optional,
	}
}

// Check performs a diagnostic check on the system toolchain binaries.
func (c *ToolchainHealthChecker) Check(ctx context.Context) (*ports.ComponentReport, error) {
	binaries := make(map[string]BinaryInfo)
	details := map[string]any{
		"binaries": binaries,
	}

	report := &ports.ComponentReport{
		Component: ports.CompToolchain,
		Status:    ports.StatusHealthy,
		Message:   "All required toolchain binaries are functional",
		Details:   details,
	}

	missingRequired := []string{}
	missingOptional := []string{}

	// Check required binaries
	for _, name := range c.required {
		info, err := c.checkBinary(ctx, name, true)
		binaries[name] = info
		if err != nil {
			missingRequired = append(missingRequired, name)
		}
	}

	// Check optional binaries
	for _, name := range c.optional {
		info, err := c.checkBinary(ctx, name, false)
		binaries[name] = info
		if err != nil {
			missingOptional = append(missingOptional, name)
		}
	}

	// Determine overall status
	if len(missingRequired) > 0 {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("Required toolchain binaries missing or non-functional: %s", strings.Join(missingRequired, ", "))
	} else if len(missingOptional) > 0 {
		report.Status = ports.StatusDegraded
		report.Message = fmt.Sprintf("Some optional toolchain binaries are missing: %s", strings.Join(missingOptional, ", "))
	}

	return report, nil
}

func (c *ToolchainHealthChecker) checkBinary(ctx context.Context, name string, required bool) (BinaryInfo, error) {
	info := BinaryInfo{IsRequired: required}

	// Step A: Path Lookup
	path, err := c.exec.LookPath(name)
	if err != nil {
		return info, err
	}
	info.Path = path

	// Step B: Execution Test
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	// Heuristic for version command
	versionArgs := []string{"--version"}
	if name == "go" {
		versionArgs = []string{"version"}
	}

	out, err := c.exec.Output(ctx, name, versionArgs...)
	if err != nil {
		// Fallback for some tools that might only support 'version'
		if name != "go" {
			out, err = c.exec.Output(ctx, name, "version")
		}
	}

	if err == nil {
		info.Version = strings.TrimSpace(string(out))
	} else {
		return info, fmt.Errorf("binary %s found but execution failed: %w", name, err)
	}

	return info, nil
}
