// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"fmt"
	"io"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"gopkg.in/yaml.v3"
)

func init() {
	register("env", func(ctx *context) command {
		return newEnvCommand(ctx)
	})
}

type envCommand struct {
	Stdout io.Writer
	Loader domain_config.ConfigLoader
}

func newEnvCommand(ctx *context) *envCommand {
	return &envCommand{
		Stdout: ctx.Stdout,
		Loader: ctx.Loader,
	}
}

func (c *envCommand) Execute(ctx stdctx.Context, args []string) error {
	// Load default config path (or parse args for -c if you want to be robust)
	configPath := "configs/assistant.yaml"
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-c" || args[i] == "--config" {
			configPath = args[i+1]
			break
		}
	}

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
