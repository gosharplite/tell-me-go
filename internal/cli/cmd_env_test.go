// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"fmt"
	"strings"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// MockLoader implements ConfigLoader for testing.
type MockLoader struct {
	Config domain_config.Config
	Err    error
}

func (m *MockLoader) Load(path string) (*domain_config.Config, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return &m.Config, nil
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
			loader := &MockLoader{Config: tt.config, Err: tt.loaderErr}
			cmdCtx := &context{
				Stdout: stdout,
				Loader: loader,
			}

			root := &cobra.Command{}
			root.PersistentFlags().StringP("config", "c", "configs/assistant.yaml", "Path to the configuration file")
			cmd := newEnvCommand(cmdCtx)
			root.AddCommand(cmd)

			if len(tt.args) > 0 {
				root.SetArgs(tt.args)
			} else {
				root.SetArgs([]string{"env"})
			}

			err := root.ExecuteContext(stdctx.Background())

			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				output := stdout.String()
				if tt.wantMask {
					if strings.Contains(output, "sk-1234567890") {
						t.Errorf("Execute() output contains unmasked API Key")
					}
					if !strings.Contains(output, "********") {
						t.Errorf("Execute() output missing redacted mask")
					}
				}

				// Verify it's valid YAML
				var decoded domain_config.Config
				if err := yaml.Unmarshal([]byte(output), &decoded); err != nil {
					t.Fatalf("Execute() output is not valid YAML: %v", err)
				}

				// Check that uppercase tags are preserved (via unmarshaling back)
				if tt.config.Mode != "" && decoded.Mode != tt.config.Mode {
					t.Errorf("Execute() YAML mismatch: got mode %q, want %q", decoded.Mode, tt.config.Mode)
				}
			}
		})
	}
}
