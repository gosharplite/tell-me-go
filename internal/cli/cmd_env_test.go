// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"errors"
	"fmt"
	"strings"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// mockLoader implements ConfigLoader for testing.
type mockLoader struct {
	Config domain_config.Config
	Err    error
}

func (m *mockLoader) Load(path string) (*domain_config.Config, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return &m.Config, nil
}

// assertEnvOutput validates the env command's YAML output.
// When checkMasking is true, it also verifies API keys are redacted.
func assertEnvOutput(t *testing.T, output string, expectedMode string, checkMasking bool) {
	t.Helper()
	if checkMasking {
		if strings.Contains(output, "sk-1234567890") {
			t.Errorf("Execute() output contains unmasked API Key")
		}
		if !strings.Contains(output, "********") {
			t.Errorf("Execute() output missing redacted mask")
		}
	}

	var decoded domain_config.Config
	if err := yaml.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("Execute() output is not valid YAML: %v", err)
	}

	if expectedMode != "" && decoded.Mode != expectedMode {
		t.Errorf("Execute() YAML mismatch: got mode %q, want %q", decoded.Mode, expectedMode)
	}
}

func TestEnvCommand_Execute(t *testing.T) {
	tests := []struct {
		name      string
		config    domain_config.Config
		loaderErr error
		args      []string
		wantMask  bool
		wantErr   bool
	}{
		{
			name: "successful output with masking",
			config: domain_config.Config{
				Mode: "cli",
				Providers: map[string]domain_config.LLMProvider{
					"openai": {
						Type:   "openai",
						APIKey: "sk-1234567890",
						Model:  "gpt-4",
					},
				},
			},
			wantMask: true,
			wantErr:  false,
		},
		{
			name:      "loader error",
			loaderErr: fmt.Errorf("failed to load"),
			wantErr:   true,
		},
		{
			name: "custom config path",
			args: []string{"env", "-c", "custom.yaml"},
			config: domain_config.Config{
				Mode: "custom",
			},
			wantErr: false,
		},
		{
			name: "custom config path with long flag",
			args: []string{"env", "--config", "custom.yaml"},
			config: domain_config.Config{
				Mode: "custom",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			loader := &mockLoader{Config: tt.config, Err: tt.loaderErr}
			cmdCtx := &context{
				Stdout: stdout,
				Loader: loader,
			}

			root := &cobra.Command{}
			root.PersistentFlags().StringP("config", "c", "configs/assistant.yaml", "Path to the configuration file")
			cmd := newEnvCommand(cmdCtx)
			root.AddCommand(cmd)

			args := tt.args
			if len(args) == 0 {
				args = []string{"env"}
			}
			root.SetArgs(args)

			err := root.ExecuteContext(stdctx.Background())

			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				assertEnvOutput(t, stdout.String(), tt.config.Mode, tt.wantMask)
			}
		})
	}
}

// failingWriter is an io.Writer that always returns a configured error.
type failingWriter struct {
	err error
}

func (w *failingWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

// TestEnvCommand_Execute_WriteError verifies that when the output writer
// fails, execute propagates the write error.
func TestEnvCommand_Execute_WriteError(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("disk full")
	stdout := &failingWriter{err: writeErr}
	loader := &mockLoader{Config: domain_config.Config{Mode: "test"}}

	c := &envCommand{
		Stdout: stdout,
		Loader: loader,
	}

	err := c.execute(stdctx.Background(), "")
	require.ErrorIs(t, err, writeErr)
}
