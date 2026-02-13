// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/stretchr/testify/require"
)

func TestSessionDependencies_Structure(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SessionDependencies{
		Paths: &persistence.Paths{
			ModeDir:         tmpDir,
			LogPath:         filepath.Join(tmpDir, "tokens.log"),
			CommandsLogPath: filepath.Join(tmpDir, "commands.log"),
		},
	}
	require.NotNil(t, deps.Paths)
}

func TestOrchestrator_Run_RequiresAgentFactory(t *testing.T) {
	orch := &Orchestrator{}
	err := orch.Run(context.TODO(), nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "AgentFactory must be set")
}

func TestSessionConfig_Structure(t *testing.T) {
	cfg := &SessionConfig{
		Config: &config.Config{
			Model: "test-model",
		},
	}
	require.Equal(t, "test-model", cfg.Config.Model)
}
