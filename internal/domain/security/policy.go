// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

// Policy defines the security rules for the agent.
type Policy struct {
	AllowedCommands        map[string]bool
	AutoApprovableCommands map[string]bool
	ForbiddenPatterns      []string
	RestrictedPaths        []string
	SafeGitSubcommands     map[string]bool
	SafeGoSubcommands      map[string]bool
}

// DefaultPolicy returns the default security policy.
func DefaultPolicy() *Policy {
	return &Policy{
		AllowedCommands: map[string]bool{
			// ...
			// Shell commands
			"go":            true,
			"git":           true,
			"ls":            true,
			"grep":          true,
			"cat":           true,
			"diff":          true,
			"whoami":        true,
			"stat":          true,
			"find":          true,
			"sh":            true,
			"make":          true,
			"npm":           true,
			"node":          true,
			"cargo":         true,
			"pytest":        true,
			"python":        true,
			"python3":       true,
			"pwd":           true,
			"echo":          true,
			"head":          true,
			"tail":          true,
			"wc":            true,
			"date":          true,
			"golangci-lint": true,
			"staticcheck":   true,
			"govulncheck":   true,
			"cp":            true,
			"mv":            true,
			"rm":            true,
			"mkdir":         true,
			"touch":         true,
			"chmod":         true,
			"chown":         true,
			"tar":           true,
			"zip":           true,
			"unzip":         true,
			"curl":          true,
			"wget":          true,

			// Filesystem Tools
			"list_files":        true,
			"get_tree":          true,
			"read_file":         true,
			"read_files":        true,
			"search_files":      true,
			"replace_text":      true,
			"find_file":         true,
			"write_file":        true,
			"append_text":       true,
			"get_file_diff":     true,
			"undo_file_change":  true,
			"register_safepath": true,
			"list_safepaths":    true,
			"remove_safepath":   true,
			"register_readpath": true,
			"list_readpaths":    true,
			"remove_readpath":   true,

			// Code Analysis Tools
			"get_definitions":          true,
			"get_file_skeleton":        true,
			"verify_architecture":      true,
			"get_code_health":          true,
			"find_usages":              true,
			"find_definitions":         true,
			"list_symbols":             true,
			"list_implementations":     true,
			"get_type_info":            true,
			"get_project_summary":      true,
			"search_usages_globally":   true,
			"get_semantic_diff":        true,
			"rename_symbol":            true,
			"list_todos":               true,
			"go_doc":                   true,
			"get_complexity_metrics":   true,
			"get_package_graph":        true,
			"analyze_sequence_flow":    true,
			"get_detailed_coverage":    true,
			"dead_code_graph":          true,
			"generate_mermaid_diagram": true,
			"move_definition":          true,
			"check_vulnerabilities":    true,
			"run_linter":               true,

			// Development Tools
			"execute_command": true,
			"pipe_commands":   true,
			"run_tests":       true,
			"go_tidy":         true,
			"get_coverage":    true,
			"run_benchmark":   true,

			// Git Tools
			"get_git_status":    true,
			"get_git_diff":      true,
			"get_git_log":       true,
			"get_git_show":      true,
			"get_git_blame":     true,
			"git_commit":        true,
			"git_create_branch": true,

			// Communication & External Tools
			"send_teams_message": true,
			"read_external_docs": true,
			"http_request":       true,
			"confluence_search":  true,
			"confluence_read":    true,
			"confluence_write":   true,
			"jira_search_issues": true,
			"jira_get_issue":     true,

			// Azure DevOps Tools
			"ado_get_pull_request":                  true,
			"ado_list_pull_requests":                true,
			"ado_get_pr_diff":                       true,
			"ado_get_pr_threads":                    true,
			"ado_get_file_content":                  true,
			"ado_list_repository_items":             true,
			"ado_list_pipelines":                    true,
			"ado_list_pipeline_runs":                true,
			"ado_get_pipeline_run":                  true,
			"ado_get_pipeline_logs":                 true,
			"ado_get_pr_statuses":                   true,
			"ado_get_pr_policy_evaluations":         true,
			"ado_list_branch_policies":              true,
			"ado_get_build_timeline":                true,
			"ado_get_task_log":                      true,
			"ado_get_build_changes":                 true,
			"ado_update_build_definition_variables": true,
			"ado_get_pipeline_definition":           true,
			"ado_create_pipeline":                   true,
			"ado_run_pipeline":                      true,

			// Session & Management Tools
			"get_session_info":         true,
			"manage_tasks":             true,
			"ask_user":                 true,
			"bypass_confirmation":      true,
			"revoke_bypass":            true,
			"estimate_cost":            true,
			"get_cost_summary":         true,
			"verify_release_readiness": true,
			"summarize_history":        true,
			"manage_history":           true,

			// Media Tools
			"create_image": true,
			"read_image":   true,
		},
		AutoApprovableCommands: map[string]bool{
			"grep": true, "ls": true, "pwd": true, "cat": true, "echo": true,
			"head": true, "tail": true, "wc": true, "stat": true, "date": true,
			"whoami": true, "diff": true, "git": true, "go": true,
			"golangci-lint": true, "staticcheck": true, "govulncheck": true,
			"confluence_search": true, "confluence_read": true,
			"ado_list_branch_policies": true, "ado_get_build_timeline": true,
			"ado_get_task_log": true, "ado_get_build_changes": true,
			"ado_create_pipeline": true, "ado_run_pipeline": true,
			"run_benchmark": true,
		},
		ForbiddenPatterns: []string{
			"&&", "||", ";", "|", ">", ">>", "<", "&", "2>", "&>", "|&", "1>", "1>>", "2>>",
		},
		SafeGitSubcommands: map[string]bool{
			"status": true, "log": true, "diff": true, "branch": true,
			"show": true, "blame": true, "ls-files": true, "rev-parse": true,
			"tag": true, "remote": true, "describe": true,
		},
		SafeGoSubcommands: map[string]bool{
			"list": true, "help": true, "version": true, "env": true,
			"vet": true, "test": true, "tool": true,
		},
	}
}

// IsCommandAllowed checks if a command is allowed.
func (p *Policy) IsCommandAllowed(cmd string) bool {
	return p.AllowedCommands[cmd]
}

// isAutoApprovable checks if a command is auto-approvable.
func (p *Policy) isAutoApprovable(cmd string) bool {
	return p.AutoApprovableCommands[cmd]
}
