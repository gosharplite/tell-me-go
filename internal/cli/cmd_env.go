// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"fmt"
	"io"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type envCommand struct {
	Stdout io.Writer
	Loader domain_config.ConfigLoader
}

func newEnvCommand(ctx *context) *cobra.Command {
	c := &envCommand{
		Stdout: ctx.Stdout,
		Loader: ctx.Loader,
	}

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Print the fully resolved configuration",
		Long:  `The env command loads the configuration, masks sensitive fields like API keys, and prints the resulting YAML to standard output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			return c.execute(cmd.Context(), configPath)
		},
	}

	return cmd
}

func (c *envCommand) execute(ctx stdctx.Context, configPath string) error {
	cfg, err := c.Loader.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if cfg.Providers != nil {
		for k, p := range cfg.Providers {
			if p.APIKey != "" {
				p.APIKey = "********"
				cfg.Providers[k] = p
			}
		}
	}

	// 2. Marshal back to YAML to preserve uppercase struct tags
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration: %w", err)
	}

	_, err = fmt.Fprintln(c.Stdout, string(data))
	return err
}
