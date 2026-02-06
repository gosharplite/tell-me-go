// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package chat

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/ui"
	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/cli/command"
	"github.com/gosharplite/tell-me-go/internal/config"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	internal_security "github.com/gosharplite/tell-me-go/internal/security"
	mediasvc "github.com/gosharplite/tell-me-go/internal/services/media"
	"github.com/gosharplite/tell-me-go/internal/tools/code"
	"github.com/gosharplite/tell-me-go/internal/tools/dev"
	"github.com/gosharplite/tell-me-go/internal/tools/files"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/gosharplite/tell-me-go/internal/tools/git"
	"github.com/gosharplite/tell-me-go/internal/tools/media"
	"github.com/gosharplite/tell-me-go/internal/tools/network"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/system"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
	"golang.org/x/term"
)

func init() {
	command.Register("chat", func(ctx *command.Context) command.Command {
		return NewCommand(ctx)
	})
}

// Command implements the main chat command.
type Command struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	HomeDir string
	SM      *internal_security.SecurityManager

	AgentFactory  func(client *api.Client, hManager *history.Manager, registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter
	ClientFactory func(cfg *config.Config, pricing pricing.PricingData) (*api.Client, error)
}

type cliOptions struct {
	configPath  string
	newSession  bool
	showVersion bool
	lastN       int
	rawOutput   bool
}

type sessionPaths struct {
	modeDir              string
	historyPath          string
	logPath              string
	commandsLogPath      string
	safePathsPath        string
	readPathsPath        string
	bypassPath           string
	persistentConfigPath string
}

// NewCommand creates a new Chat Command with default factories.
func NewCommand(ctx *command.Context) *Command {
	return &Command{
		Version: ctx.Version,
		Stdin:   ctx.Stdin,
		Stdout:  ctx.Stdout,
		Stderr:  ctx.Stderr,
		HomeDir: ctx.HomeDir,
		SM:      ctx.SM,
		AgentFactory: func(client *api.Client, hManager *history.Manager, reg domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter {
			return agent.New(client, hManager, reg, sm, disableStreaming,
				agent.WithPricing(model, mode, pricingOverrides),
				agent.WithSessionCostTracker(tracker),
			)
		},
		ClientFactory: func(cfg *config.Config, pricing pricing.PricingData) (*api.Client, error) {
			authenticator := &auth.VertexAuth{}
			maxBudget := cfg.ResolveThinkingBudget(cfg.Model, pricing)
			return api.NewClient(cfg.URL, cfg.Model, authenticator, cfg.ThinkingBudget, cfg.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch)
		},
	}
}

// Execute runs the chat command logic.
func (c *Command) Execute(ctx context.Context, args []string) error {
	// Sync security manager with current command stdin
	c.SM.SetInputReader(c.Stdin)

	// 1. Initialize Configuration
	opts, fs, cfg, err := c.initializeConfiguration(args)
	if err != nil {
		return err
	}
	if opts.showVersion {
		fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
		return nil
	}

	// 2. Handle Prompt Early
	prompt, err := c.capturePrompt(ctx, fs, opts.lastN)
	if err != nil {
		return err
	}

	// 3. Prepare Session Paths
	paths, err := c.prepareSessionPaths(cfg)
	if err != nil {
		return err
	}

	// 4. Initialize Security & Session
	pricingOverrides := c.getPricingOverrides(cfg)
	c.setupSessionState(&paths, opts, cfg, pricingOverrides)

	// 5. Initialize Dependencies
	hManager, client, registry, tracker, pruned, pricing, err := c.initializeDependencies(ctx, paths, cfg, pricingOverrides)
	if err != nil {
		return err
	}

	// 6. Handle History & Early Exit
	if c.handleHistoryAndExit(prompt, opts, hManager, cfg) {
		return nil
	}

	// 7. Setup Agent & Execute
	chatAgent := c.AgentFactory(client, hManager, registry, c.SM, cfg.DisableStreaming, cfg.Model, cfg.Mode, pricingOverrides, tracker)
	c.applyConfiguration(chatAgent, cfg, opts, &paths, pruned, pricing)

	sess := agent.NewSession(hManager)
	sess.PrunedTurns = pruned

	if err := chatAgent.Chat(ctx, sess, prompt); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return c.finalizeSession(ctx, chatAgent, hManager, paths, cfg, pricingOverrides)
}

func (c *Command) initializeConfiguration(args []string) (*cliOptions, *flag.FlagSet, *config.Config, error) {
	args = c.sanitizeArgs(args)
	opts, fs, err := c.parseFlags(args[1:])
	if err != nil {
		return nil, nil, nil, err
	}
	if opts.showVersion {
		return opts, fs, nil, nil
	}

	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	return opts, fs, cfg, nil
}

func (c *Command) prepareSessionPaths(cfg *config.Config) (sessionPaths, error) {
	paths, err := c.initPaths(cfg)
	if err != nil {
		return sessionPaths{}, err
	}

	_, err = c.loadPersistentConfig(paths, cfg)
	if err != nil {
		log.Printf("Warning: Failed to load/update persistent config: %v", err)
	}

	return *paths, nil
}

func (c *Command) initializeDependencies(ctx context.Context, paths sessionPaths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) (*history.Manager, *api.Client, domaintools.IToolRegistry, domain_pricing.ICostTracker, int, pricing.PricingData, error) {
	hManager := history.NewManager(paths.historyPath)
	if err := hManager.Load(ctx); err != nil {
		return nil, nil, nil, nil, 0, pricing.PricingData{}, fmt.Errorf("error loading history: %w", err)
	}
	pruned, _ := hManager.Prune(ctx, cfg.MaxHistoryTurns)

	pricingData := framework.GetPricing(ctx, c.SM, filepath.Join(c.HomeDir, "output"))

	hManager.Snapshot()

	client, err := c.ClientFactory(cfg, pricingData)
	if err != nil {
		return nil, nil, nil, nil, 0, pricingData, fmt.Errorf("error creating client: %w", err)
	}

	registry := c.setupRegistry(client, cfg, &paths, pricingOverrides)

	modelPricing := framework.GetModelPricing(cfg.Model, pricingData)
	tracker := framework.NewSessionCostTracker(c.SM, paths.logPath, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	return hManager, client, registry, tracker, pruned, pricingData, nil
}

func (c *Command) finalizeSession(ctx context.Context, chatAgent agent.Chatter, hManager *history.Manager, paths sessionPaths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) error {
	if err := hManager.Save(ctx); err != nil {
		return fmt.Errorf("error saving history: %w", err)
	}

	if err := framework.RecordSessionCost(ctx, c.SM, chatAgent.GetCostTracker(), paths.logPath, cfg.Model, cfg.Mode, "", pricingOverrides); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to record final session cost: %v\n", err)
	}

	return nil
}

func (c *Command) getPricingOverrides(cfg *config.Config) map[string]pricing.ModelPricing {
	pricingOverrides := make(map[string]pricing.ModelPricing)
	for k, v := range cfg.Models {
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}
	return pricingOverrides
}

func (c *Command) setupSessionState(paths *sessionPaths, opts *cliOptions, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) {
	c.setupSecurity(paths, opts, cfg)
	if opts.newSession {
		c.handleNewSession(paths, cfg, pricingOverrides)
	}
}

func (c *Command) handleHistoryAndExit(prompt string, opts *cliOptions, hManager *history.Manager, cfg *config.Config) bool {
	if opts.lastN > 0 {
		c.showHistory(hManager, opts.lastN, opts.rawOutput, cfg.ShowThoughts)
	}

	return prompt == "" && opts.lastN > 0
}

func (c *Command) initPaths(cfg *config.Config) (*sessionPaths, error) {
	modeDir := filepath.Join(c.HomeDir, "output", cfg.Mode)
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session directory [%s]: %v", modeDir, err)
	}

	return &sessionPaths{
		modeDir:              modeDir,
		historyPath:          filepath.Join(modeDir, "history.json"),
		logPath:              filepath.Join(modeDir, "tokens.log"),
		commandsLogPath:      filepath.Join(modeDir, "commands.log"),
		safePathsPath:        filepath.Join(modeDir, "safepaths.json"),
		readPathsPath:        filepath.Join(modeDir, "readpaths.json"),
		bypassPath:           filepath.Join(modeDir, "bypass.log"),
		persistentConfigPath: filepath.Join(modeDir, "config.json"),
	}, nil
}

func (c *Command) loadPersistentConfig(paths *sessionPaths, cfg *config.Config) (map[string]string, error) {
	pCfg := make(map[string]string)
	data, err := os.ReadFile(paths.persistentConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return pCfg, nil // No overrides yet, this is fine
		}
		return nil, err
	}

	if err := json.Unmarshal(data, &pCfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal persistent config: %w", err)
	}

	return pCfg, nil
}

func (c *Command) setupSecurity(paths *sessionPaths, opts *cliOptions, cfg *config.Config) {
	c.SM.SetSafePathsFile(paths.safePathsPath)
	c.SM.SetReadOnlyPathsFile(paths.readPathsPath)
	c.SM.SetBypassFile(paths.bypassPath)
	c.SM.SetCommandsLogFile(paths.commandsLogPath)

	if err := c.SM.LoadSafePaths(); err != nil {
		log.Printf("Warning: Failed to load persistent safe paths: %v", err)
	}
	if err := c.SM.LoadReadOnlyPaths(); err != nil {
		log.Printf("Warning: Failed to load persistent read-only paths: %v", err)
	}
	c.SM.LoadBypassState()

	c.SM.RegisterSafePath(filepath.Join(c.HomeDir, "output"))
	c.SM.RegisterReadOnlyPath(opts.configPath)
}

func (c *Command) handleNewSession(paths *sessionPaths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.logPath))
	_ = framework.RecordSessionCost(context.Background(), c.SM, nil, paths.logPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides)
	c.archiveSessionFilesWithTimestamp(c.HomeDir, timestamp, paths.historyPath, paths.logPath, paths.commandsLogPath)
	c.cleanupOldBackups(c.HomeDir, cfg.Mode)
}

func (c *Command) setupRegistry(client *api.Client, cfg *config.Config, paths *sessionPaths, pricingOverrides map[string]pricing.ModelPricing) *registry.Registry {
	reg := registry.New()

	files.Register(reg, c.SM)
	code.Register(reg, c.SM)
	system.Register(reg, c.SM)
	git.Register(reg, c.SM)
	dev.Register(reg, c.SM)
	network.Register(reg, c.SM)
	framework.RegisterState(reg, c.SM, paths.modeDir)
	framework.RegisterPolicy(reg, c.SM)
	framework.RegisterMetrics(reg, c.SM, paths.logPath, cfg.Model, cfg.Mode, pricingOverrides)
	dev.RegisterRelease(reg, c.SM)
	media.Register(reg, c.SM, mediasvc.NewService(client, filepath.Join(c.HomeDir, "assets/generated")))

	return reg
}

func (c *Command) applyConfiguration(chatAgent agent.Chatter, cfg *config.Config, opts *cliOptions, paths *sessionPaths, pruned int, pricing pricing.PricingData) {
	chatAgent.SetPersistentConfigPath(paths.persistentConfigPath)
	chatAgent.SetMainConfigPath(opts.configPath)
	chatAgent.SetLogFile(paths.logPath)

	renderer := ui.NewStdUIRenderer(c.SM)
	renderer.SetWriters(c.Stdout, c.Stderr)
	// Note: We need to handle UISubscriber. It was in the cli package.
	// I'll move it to a shared place or this package.
	subscriber := NewUISubscriber(renderer, cfg.ShowThoughts, cfg.ShowTools, opts.rawOutput, paths.logPath)
	chatAgent.Subscribe(subscriber.HandleEvent)

	maxTokens := cfg.MaxHistoryTokens
	if mCfg, ok := cfg.Models[cfg.Model]; ok && mCfg.ContextWindow > 0 {
		if maxTokens > mCfg.ContextWindow {
			maxTokens = mCfg.ContextWindow
		}
	} else {
		for k, v := range cfg.Models {
			if k != "default" && strings.Contains(cfg.Model, k) && v.ContextWindow > 0 {
				if maxTokens > v.ContextWindow {
					maxTokens = v.ContextWindow
				}
				break
			}
		}
	}

	threshold := config.DefaultTieredThreshold
	if mPricing, ok := pricing.Models[cfg.Model]; ok && mPricing.TieredThreshold > 0 {
		threshold = int(mPricing.TieredThreshold)
	} else {
		for k, v := range pricing.Models {
			if k != "default" && strings.Contains(cfg.Model, k) && v.TieredThreshold > 0 {
				threshold = int(v.TieredThreshold)
				break
			}
		}
	}

	chatAgent.SetLimits(cfg.MaxToolTurns, maxTokens, cfg.MaxHistoryTurns)
	chatAgent.SetTieredThreshold(threshold)
	chatAgent.SetPrunedTurns(pruned)
	chatAgent.SetConcurrency(cfg.MaxConcurrentTools, cfg.ToolTimeoutSeconds)
}

func (c *Command) archiveSessionFilesWithTimestamp(homeDir, timestamp string, filesToMove ...string) {
	backupDir := filepath.Join(homeDir, "output", "backups", timestamp)

	backupCreated := false
	for _, f := range filesToMove {
		if _, err := os.Stat(f); err == nil {
			if !backupCreated {
				if err := os.MkdirAll(backupDir, 0755); err != nil {
					func() {
						c.SM.TerminalLock()
						defer c.SM.TerminalUnlock()
						fmt.Fprintf(c.Stderr, "Error creating backup directory: %v\n", err)
					}()
					return
				}
				func() {
					c.SM.TerminalLock()
					defer c.SM.TerminalUnlock()
					fmt.Fprintf(c.Stdout, "Archiving existing session files to %s\n", backupDir)
				}()
				backupCreated = true
			}
			dest := filepath.Join(backupDir, filepath.Base(f))
			if err := os.Rename(f, dest); err != nil {
				func() {
					c.SM.TerminalLock()
					defer c.SM.TerminalUnlock()
					fmt.Fprintf(c.Stderr, "Error archiving %s: %v\n", f, err)
				}()
			}
		}
	}
}

func (c *Command) cleanupOldBackups(homeDir, mode string) {
	backupBaseDir := filepath.Join(homeDir, "output", "backups")
	entries, err := os.ReadDir(backupBaseDir)
	if err != nil {
		return
	}

	retentionDays := 30
	configPath := filepath.Join(homeDir, "output", mode, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg map[string]string
		if err := json.Unmarshal(data, &cfg); err == nil {
			if val, ok := cfg["backup_retention_days"]; ok {
				if days, err := strconv.Atoi(val); err == nil {
					retentionDays = days
				}
			}
		}
	}

	if retentionDays <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if len(entry.Name()) < 15 {
			continue
		}

		folderTime, err := time.Parse("20060102_150405", entry.Name()[:15])
		if err != nil {
			continue
		}

		if folderTime.Before(cutoff) {
			path := filepath.Join(backupBaseDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				func() {
					c.SM.TerminalLock()
					defer c.SM.TerminalUnlock()
					fmt.Fprintf(c.Stderr, "Warning: Failed to cleanup old backup %s: %v\n", path, err)
				}()
			}
		}
	}
}

func (c *Command) parseFlags(args []string) (*cliOptions, *flag.FlagSet, error) {
	fs := flag.NewFlagSet("tell-me-go", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	opts := &cliOptions{}
	fs.StringVar(&opts.configPath, "c", "configs/vertex.yaml", "Path to the configuration file")
	fs.BoolVar(&opts.newSession, "new", false, "Start a new session")
	fs.BoolVar(&opts.showVersion, "v", false, "Show version information")
	fs.IntVar(&opts.lastN, "l", 0, "Show the last N messages from history")
	fs.BoolVar(&opts.rawOutput, "r", false, "Show raw output (without markdown rendering)")

	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}

	return opts, fs, nil
}

func (c *Command) sanitizeArgs(args []string) []string {
	if len(args) < 2 {
		return args
	}

	processed := args[1:]
	for i, arg := range processed {
		if arg == "-l" {
			isNextNum := false
			if i+1 < len(processed) {
				if _, err := strconv.Atoi(processed[i+1]); err == nil {
					isNextNum = true
				}
			}

			if !isNextNum {
				newArgs := make([]string, 0, len(args)+1)
				newArgs = append(newArgs, args[:i+2]...)
				newArgs = append(newArgs, "1")
				newArgs = append(newArgs, args[i+2:]...)
				return newArgs
			}
		}
	}
	return args
}

func (c *Command) capturePrompt(ctx context.Context, fs *flag.FlagSet, lastN int) (string, error) {
	prompt := strings.Join(fs.Args(), " ")

	if val := os.Getenv("TELL_ME_MOCK_PROMPT"); val != "" {
		return val, nil
	}

	var fd int = -1
	if f, ok := c.Stdin.(*os.File); ok {
		fd = int(f.Fd())
	}
	isTerminal := fd != -1 && term.IsTerminal(fd)

	if !isTerminal {
		readChan := make(chan []byte, 1)
		go func() {
			b, _ := io.ReadAll(c.Stdin)
			readChan <- b
		}()

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case b := <-readChan:
			if len(b) > 0 {
				if prompt != "" {
					prompt = prompt + "\n" + string(b)
				} else {
					prompt = string(b)
				}
			}
		}
	} else if prompt == "" && lastN == 0 {
		func() {
			c.SM.TerminalLock()
			defer c.SM.TerminalUnlock()
			fmt.Fprintf(c.Stdout, "%s[Reading multi-line input. Press Ctrl+D to send]%s\n", colors.ColorYellow, colors.ColorReset)
		}()

		readChan := make(chan []byte, 1)
		go func() {
			b, _ := io.ReadAll(c.Stdin)
			readChan <- b
		}()

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case b := <-readChan:
			prompt = string(b)
		}
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		if lastN > 0 {
			return "", nil
		}
		fmt.Fprintln(c.Stderr, "Usage: tell-me-go [flags] <prompt>")
		fs.PrintDefaults()
		return "", fmt.Errorf("empty prompt")
	}
	func() {
		c.SM.TerminalLock()
		defer c.SM.TerminalUnlock()
		fmt.Fprintf(c.Stderr, "%s[%s] Input captured. Processing...%s\n", colors.ColorGreen, time.Now().Format("15:04:05"), colors.ColorReset)
	}()
	return prompt, nil
}

func (c *Command) showHistory(hManager *history.Manager, n int, raw bool, showThoughts bool) {
	contents := hManager.GetContents()
	if len(contents) == 0 {
		fmt.Fprintln(c.Stdout, "No history found.")
		return
	}

	if n > len(contents) {
		n = len(contents)
	}

	start := len(contents) - n
	var r *glamour.TermRenderer
	if !raw {
		r, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithEmoji(),
		)
	}

	for i := start; i < len(contents); i++ {
		c2 := contents[i]
		roleColor := colors.ColorBlue
		if c2.Role != "user" {
			roleColor = colors.ColorMagenta
		}
		fmt.Fprintf(c.Stdout, "%s[%s]%s\n", roleColor, strings.ToUpper(c2.Role), colors.ColorReset)
		for _, p := range c2.Parts {
			if p.Thought && !showThoughts {
				continue
			}

			if p.Text != "" {
				if raw || r == nil {
					fmt.Fprint(c.Stdout, p.Text)
					if !strings.HasSuffix(p.Text, "\n") {
						fmt.Fprintln(c.Stdout)
					}
				} else {
					out, err := r.Render(p.Text)
					if err != nil {
						fmt.Fprintln(c.Stdout, p.Text)
					} else {
						fmt.Fprint(c.Stdout, out)
					}
				}
			}
			if p.FunctionCall != nil {
				fmt.Fprintf(c.Stdout, "%s[Tool Call] %s%s\n", colors.ColorCyan, p.FunctionCall.Name, colors.ColorReset)
			}
			if p.FunctionResponse != nil {
				fmt.Fprintf(c.Stdout, "%s[Tool Response] %s%s\n", colors.ColorCyan, p.FunctionResponse.Name, colors.ColorReset)
			}
		}
		fmt.Fprintln(c.Stdout)
	}
}
