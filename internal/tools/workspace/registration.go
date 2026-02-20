// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type fileSystemManager struct {
	reader *fileReader
	writer *fileWriter
	search *fileSearcher
}

// Register adds all workspace-related tools (file, git, system) to the registry.
func Register(r tools.IToolRegistry, sm domain_security.ISecurityManager, exec tools.CommandExecutor, validator domain_security.ICommandValidator) {
	registerFiles(r, sm)
	registerSystem(r, sm, validator)
	registerGit(r, sm, exec)
}

func registerFiles(r tools.IToolRegistry, sm domain_security.ISecurityManager) {
	bm := newBackupManager(sm, 10)
	m := &fileSystemManager{
		reader: &fileReader{sm: sm, fs: persistence.DefaultFileSystem},
		writer: &fileWriter{sm: sm, bm: bm, fs: persistence.DefaultFileSystem},
		search: &fileSearcher{sm: sm, fs: persistence.DefaultFileSystem},
	}

	r.Register(&tools.ToolDeclaration{
		Name:        "list_files",
		Description: "Returns a shallow list of filenames and directory names (similar to 'ls') in a specific path. Useful for confirming file existence before reading.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to list (defaults to current directory '.')",
				},
			},
		},
	}, m.reader.listFiles)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_tree",
		Description: "Returns a visual directory tree structure.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to list (defaults to current directory '.')",
				},
				"max_depth": {
					Type:        "INTEGER",
					Description: "Depth of the tree (default 2)",
				},
			},
		},
	}, m.reader.getTree)

	r.Register(&tools.ToolDeclaration{
		Name:        "read_file",
		Description: "Reads the full content of a specific file (similar to 'cat').",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file to read.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.reader.readFile)

	r.Register(&tools.ToolDeclaration{
		Name:        "search_files",
		Description: "Performs a recursive regex search for a text pattern within a specific subdirectory. Use this when the search scope is restricted to a known module or folder.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
				"query": {
					Type:        "STRING",
					Description: "The string or regex to search for.",
				},
			},
			Required: []string{"query"},
		},
	}, m.search.searchFiles)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "replace_text",
		Description: "Replaces the first occurrence of a specific text block in a file with new content.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file to edit.",
				},
				"old_text": {
					Type:        "STRING",
					Description: "The exact text block to find and replace.",
				},
				"new_text": {
					Type:        "STRING",
					Description: "The new text to insert.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for this replacement.",
				},
			},
			Required: []string{"filepath", "old_text", "new_text", "reason"},
		},
	}, m.writer.replaceText, tools.ToolOptions{Serial: true, LongRunning: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "find_file",
		Description: "Finds files based on name patterns using filepath.Match (e.g., '*.go'). Useful for locating specific configuration or source files by name.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to start the search (defaults to '.')",
				},
				"pattern": {
					Type:        "STRING",
					Description: "The file name pattern to search for (e.g., 'config.*').",
				},
			},
			Required: []string{"pattern"},
		},
	}, m.reader.findFile)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_definitions",
		Description: "Performs a regex-based search for symbol declarations (func, type, class) across files. Faster than AST tools for broad navigation but may return false positives.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to search.",
				},
				"query": {
					Type:        "STRING",
					Description: "Optional name pattern to filter definitions (regex).",
				},
			},
		},
	}, m.search.grepDefinitions)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "write_file",
		Description: "Creates a new file or overwrites an existing one with the provided content. Automatically creates parent directories.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file to write.",
				},
				"content": {
					Type:        "STRING",
					Description: "The full content to write to the file.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for writing this file.",
				},
			},
			Required: []string{"filepath", "content", "reason"},
		},
	}, m.writer.writeFile, tools.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "append_text",
		Description: "Appends text to the end of a file. Efficient for logs or lists; avoids reading the whole file.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file.",
				},
				"content": {
					Type:        "STRING",
					Description: "The text to append. Ensure you include a leading newline (\\n) if starting a new line.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for appending this text.",
				},
			},
			Required: []string{"filepath", "content", "reason"},
		},
	}, m.writer.appendText, tools.ToolOptions{Serial: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "get_file_diff",
		Description: "Generates a standard unified diff between two arbitrary file paths on disk. Does not require Git history.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"file1": {
					Type:        "STRING",
					Description: "The first file to compare.",
				},
				"file2": {
					Type:        "STRING",
					Description: "The second file to compare.",
				},
			},
			Required: []string{"file1", "file2"},
		},
	}, m.reader.getFileDiff)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "undo_file_change",
		Description: "Reverts the last N file modifications (WRITE or REPLACE actions).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"n": {
					Type:        "INTEGER",
					Description: "Number of changes to revert (default 1).",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for reverting the changes.",
				},
			},
			Required: []string{"reason"},
		},
	}, m.writer.undoFileChange, tools.ToolOptions{Serial: true})
}

func registerSystem(r tools.IToolRegistry, sm domain_security.ISecurityManager, validator domain_security.ICommandValidator) {
	shell := newshellTool(sm, validator)
	interaction := newinteractionTool(sm)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "execute_command",
		Description: "Executes a single shell command as if in a terminal without shell interpretation (direct binary call). Security: Only whitelisted commands are auto-approved; others require user confirmation.",
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
	}, shell.ExecuteCommand, tools.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "pipe_commands",
		Description: "Executes a sequence of commands by piping the output of each to the next. Security: All commands in the pipe must be whitelisted for auto-approval.",
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
	}, shell.PipeCommands, tools.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, interaction.askUser, tools.ToolOptions{Serial: true, LongRunning: true})
}

func registerGit(r tools.IToolRegistry, sm domain_security.ISecurityManager, exec tools.CommandExecutor) {
	m := &gitManager{sm: sm, Exec: exec}

	r.Register(&tools.ToolDeclaration{
		Name:        "get_git_status",
		Description: "Retrieves the short status of the git repository (staged, unstaged, and untracked files).",
	}, m.getGitStatus)

	r.Register(&tools.ToolDeclaration{
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
	}, m.getGitDiff)

	r.Register(&tools.ToolDeclaration{
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
	}, m.getGitLog)

	r.Register(&tools.ToolDeclaration{
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
	}, m.getGitCommit)

	r.Register(&tools.ToolDeclaration{
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
	}, m.getGitBlame)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "git_commit",
		Description: "Commits staged changes with a message.",
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
	}, m.gitCommit, tools.ToolOptions{Serial: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "git_create_branch",
		Description: "Creates and checks out a new git branch.",
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
	}, m.gitCreateBranch, tools.ToolOptions{Serial: true})
}

// RegisterPersistence adds persistence tools to the registry.
func RegisterPersistence(r tools.IToolRegistry, state services.ISessionProvider) {
	newpersistenceTools(state).Register(r)
}
