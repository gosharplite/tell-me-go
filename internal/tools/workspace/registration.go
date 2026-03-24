// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type fileSystemManager struct {
	reader *fileReader
	writer *fileWriter
	search *fileSearcher
}

// Register adds all workspace-related tools (file, git, system) to the registry.
func Register(r tools.Registry, sm domain_security.Manager, exec tools.CommandExecutor, validator domain_security.CommandValidator, fs persistence.FileSystem) error {
	if err := registerFiles(r, sm, fs); err != nil {
		return err
	}
	if err := registerSystem(r, sm, validator); err != nil {
		return err
	}
	if err := registerGit(r, sm, exec); err != nil {
		return err
	}
	return nil
}

func registerFiles(r tools.Registry, sm domain_security.Manager, fs persistence.FileSystem) error {
	bm := newBackupManager(sm, fs, 10)
	m := &fileSystemManager{
		reader: &fileReader{sm: sm, fs: fs},
		writer: &fileWriter{sm: sm, bm: bm, fs: fs},
		search: &fileSearcher{sm: sm, fs: fs},
	}

	type toolSpec struct {
		decl    *tools.ToolDeclaration
		handler tools.ToolFunc
		opts    *tools.ToolOptions
	}

	specs := []toolSpec{
		{
			decl: &tools.ToolDeclaration{
				Name:        "list_files",
				Description: "Returns a shallow list of filenames and directory names (similar to 'ls') in a specific path. Useful for confirming file existence before reading.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {Type: "STRING", Description: "The directory path to list (defaults to current directory '.')"},
					},
				},
			},
			handler: m.reader.listFiles,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_tree",
				Description: "Returns a visual directory tree structure.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path":      {Type: "STRING", Description: "The directory path to list (defaults to current directory '.')"},
						"max_depth": {Type: "INTEGER", Description: "Depth of the tree (default 2)"},
					},
				},
			},
			handler: m.reader.getTree,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "read_file",
				Description: "[DEPRECATED] Reads the content of a single file. You MUST use 'read_files' instead (even for single files) to maintain consistent batching patterns and minimize LLM roundtrips.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"filepath": {Type: "STRING", Description: "The path to the file to read."},
					},
					Required: []string{"filepath"},
				},
			},
			handler: m.reader.readFile,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "read_files",
				Description: "Reads the full content of multiple files. CRITICAL: Use this tool whenever you need to read more than one file to minimize LLM roundtrips.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"filepaths": {
							Type:        "ARRAY",
							Items:       &tools.Schema{Type: "STRING"},
							Description: "The list of file paths to read.",
						},
					},
					Required: []string{"filepaths"},
				},
			},
			handler: m.reader.readFiles,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "search_files",
				Description: "Performs a recursive regex search for a text pattern within a specific subdirectory. Use this when the search scope is restricted to a known module or folder.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path":     {Type: "STRING", Description: "The directory to search (defaults to '.')"},
						"query":    {Type: "STRING", Description: "The text or regex to search for."},
						"is_regex": {Type: "BOOLEAN", Description: "If true, treats query as a regex. Default is false (literal string search)."},
					},
					Required: []string{"query"},
				},
			},
			handler: m.search.searchFiles,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:            "replace_text",
				Description:     "Replaces the first occurrence of a specific text block in a file with new content.",
				RequiresConsent: true,
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"filepath": {Type: "STRING", Description: "The path to the file to edit."},
						"old_text": {Type: "STRING", Description: "The exact text block to find and replace."},
						"new_text": {Type: "STRING", Description: "The new text to insert."},
						"reason":   {Type: "STRING", Description: "Reason for this replacement."},
					},
					Required: []string{"filepath", "old_text", "new_text", "reason"},
				},
			},
			handler: m.writer.replaceText,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "find_file",
				Description: "Finds files based on name patterns using filepath.Match (e.g., '*.go'). Useful for locating specific configuration or source files by name.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path":    {Type: "STRING", Description: "The directory path to start the search (defaults to '.')"},
						"pattern": {Type: "STRING", Description: "The file name pattern to search for (e.g., 'config.*')."},
					},
					Required: []string{"pattern"},
				},
			},
			handler: m.reader.findFile,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_definitions",
				Description: "Performs a regex-based search for symbol declarations (func, type, class) across files. Faster than AST tools for broad navigation but may return false positives. HINT: Once you find the files containing the definitions, use 'read_files' to view the internal logic.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path":  {Type: "STRING", Description: "The directory path to search."},
						"query": {Type: "STRING", Description: "Optional name pattern to filter definitions (regex)."},
					},
				},
			},
			handler: m.search.grepDefinitions,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:            "write_file",
				Description:     "Creates a new file or overwrites an existing one with the provided content. Automatically creates parent directories.",
				RequiresConsent: true,
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"filepath": {Type: "STRING", Description: "The path to the file to write."},
						"content":  {Type: "STRING", Description: "The full content to write to the file."},
						"reason":   {Type: "STRING", Description: "Reason for writing this file."},
					},
					Required: []string{"filepath", "content", "reason"},
				},
			},
			handler: m.writer.writeFile,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:            "append_text",
				Description:     "Appends text to the end of a file. Efficient for logs or lists; avoids reading the whole file.",
				RequiresConsent: true,
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"filepath": {Type: "STRING", Description: "The path to the file."},
						"content":  {Type: "STRING", Description: "The text to append. Ensure you include a leading newline (\\n) if starting a new line."},
						"reason":   {Type: "STRING", Description: "Reason for appending this text."},
					},
					Required: []string{"filepath", "content", "reason"},
				},
			},
			handler: m.writer.appendText,
			opts:    &tools.ToolOptions{Serial: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_file_diff",
				Description: "Generates a standard unified diff between two arbitrary file paths on disk. Does not require Git history.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"file1": {Type: "STRING", Description: "The first file to compare."},
						"file2": {Type: "STRING", Description: "The second file to compare."},
					},
					Required: []string{"file1", "file2"},
				},
			},
			handler: m.reader.getFileDiff,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "undo_file_change",
				Description: "Reverts the last N file modifications (WRITE or REPLACE actions).",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"n":      {Type: "INTEGER", Description: "Number of changes to revert (default 1)."},
						"reason": {Type: "STRING", Description: "Reason for reverting the changes."},
					},
					Required: []string{"reason"},
				},
			},
			handler: m.writer.undoFileChange,
			opts:    &tools.ToolOptions{Serial: true},
		},
	}

	for _, spec := range specs {
		if spec.opts != nil {
			if err := r.RegisterWithOptions(spec.decl, spec.handler, *spec.opts); err != nil {
				return err
			}
		} else {
			if err := r.Register(spec.decl, spec.handler); err != nil {
				return err
			}
		}
	}

	return nil
}

func registerSystem(r tools.Registry, sm domain_security.Manager, validator domain_security.CommandValidator) error {
	shell := newshellTool(sm, validator)
	interaction := newinteractionTool(sm)

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:            "execute_command",
		Description:     "Executes a command. Shell features like operators (&&, ||, ;, |, >, <), wildcards (*, ?), and variable expansion ($) are supported and automatically handled via 'sh -c'. Security: Only whitelisted commands are auto-approved. For advanced multi-stage pipes, use the 'pipe_commands' tool.",
		RequiresConsent: true,
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"command": {
					Type:        "STRING",
					Description: "The shell command to execute (e.g., 'ls -la', 'go test ./...').",
				},
				"reason": {
					Type:        "STRING",
					Description: "A short explanation of why this command needs to be executed.",
				},
				"output_file": {
					Type:        "STRING",
					Description: "Optional: Redirect output to this file.",
				},
				"append": {
					Type:        "BOOLEAN",
					Description: "Optional: If output_file is set, append to it instead of overwriting.",
				},
			},
			Required: []string{"command", "reason"},
		},
	}, shell.ExecuteCommand, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:            "pipe_commands",
		Description:     "Executes a sequence of commands by piping the output of each to the next. Security: All commands in the pipe must be whitelisted for auto-approval.",
		RequiresConsent: true,
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"commands": {
					Type: "ARRAY",
					Items: &tools.Schema{
						Type: "STRING",
					},
					Description: "The sequence of commands to pipe (e.g., ['ls -la', 'grep .go']).",
				},
				"reason": {
					Type:        "STRING",
					Description: "A short explanation of why this pipeline needs to be executed.",
				},
				"output_file": {
					Type:        "STRING",
					Description: "Optional: Redirect the final output to this file.",
				},
				"append": {
					Type:        "BOOLEAN",
					Description: "Optional: If output_file is set, append to it instead of overwriting.",
				},
			},
			Required: []string{"commands", "reason"},
		},
	}, shell.PipeCommands, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "ask_user",
		Description: "Asks the user a specific question to clarify requirements or request confirmation.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"question": {
					Type:        "STRING",
					Description: "The question to ask the user.",
				},
			},
			Required: []string{"question"},
		},
	}, interaction.askUser, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}
	return nil
}

func registerGit(r tools.Registry, sm domain_security.Manager, exec tools.CommandExecutor) error {
	m := &gitManager{sm: sm, Exec: exec}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "get_git_status",
		Description: "Retrieves the short status of the git repository (staged, unstaged, and untracked files).",
	}, m.getGitStatus); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "get_git_diff",
		Description: "Retrieves the git diff between the working directory (or staged index) and the last commit. Use this to review changes before committing.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"staged": {
					Type:        "BOOLEAN",
					Description: "If true, shows staged changes.",
				},
			},
		},
	}, m.getGitDiff); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "get_git_log",
		Description: "Retrieves the git commit log.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"limit": {
					Type:        "INTEGER",
					Description: "Number of commits to show (default: 10).",
				},
			},
		},
	}, m.getGitLog); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "get_git_show",
		Description: "Shows the full details (diff and metadata) of a specific commit hash (runs git show).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"hash": {
					Type:        "STRING",
					Description: "The commit hash to inspect.",
				},
			},
			Required: []string{"hash"},
		},
	}, m.getGitCommit); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "get_git_blame",
		Description: "Shows who changed which lines in a file.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.getGitBlame); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:            "git_commit",
		Description:     "Commits currently staged changes with a message. You MUST stage files first using 'execute_command' with 'git add <files>' before calling this tool.",
		RequiresConsent: true,
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"message": {
					Type:        "STRING",
					Description: "The commit message.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for this commit (architectural intent).",
				},
			},
			Required: []string{"message", "reason"},
		},
	}, m.gitCommit, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:            "git_create_branch",
		Description:     "Creates and checks out a new git branch.",
		RequiresConsent: true,
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"name": {
					Type:        "STRING",
					Description: "The name of the new branch.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for creating this branch.",
				},
			},
			Required: []string{"name", "reason"},
		},
	}, m.gitCreateBranch, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}
	return nil
}

// RegisterPersistence adds persistence tools to the registry.
func RegisterPersistence(r tools.Registry, state ports.SessionProvider) error {
	return newpersistenceTools(state).Register(r)
}
