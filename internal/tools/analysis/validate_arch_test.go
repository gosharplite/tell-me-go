// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"os/exec"
	"strings"
)

type realAnalysisGoRunner struct {
	mockAnalysisGoRunner
}

func (r *realAnalysisGoRunner) GetPackageList(ctx context.Context, path string) ([]byte, error) {
	dir, _ := r.GetModuleDir(ctx)
	cmd := exec.CommandContext(ctx, "go", "list", "-json", path)
	cmd.Dir = dir
	return cmd.Output()
}

func (r *realAnalysisGoRunner) GetModulePath(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "list", "-m").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *realAnalysisGoRunner) GetModuleDir(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
