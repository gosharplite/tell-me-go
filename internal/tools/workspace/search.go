// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type fileSearcher struct {
	sm domain_security.PathValidator
	fs persistence.FileSystem
}

// defPatterns defines regex patterns for detecting definitions in supported languages.
var defPatterns = []string{
	`^def\s+\w+`,              // Python function
	`^class\s+\w+`,            // Python/JS class
	`^function\s+`,            // JS function
	`^const\s+\w+\s*=\s*\(`,   // JS arrow function
	`^\w+\(\)\s*\{`,           // Bash function
	`^func\s+`,                // Go function
	`^type\s+\w+\s+struct`,    // Go struct
	`^type\s+\w+\s+interface`, // Go interface
}

func (s *fileSearcher) searchFiles(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path    string `json:"path"`
		Query   string `json:"query"`
		IsRegex bool   `json:"is_regex"`
		Reason  string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Query == "" {
		return tools.ToolResult{}, fmt.Errorf("query argument is required")
	}

	path := s.getPathOrDefault(params.Path)
	matcher, err := s.createSearchMatcher(params.Query, params.IsRegex)
	if err != nil {
		return tools.ToolResult{}, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resChan, errChan := ConcurrentSearch(ctx, s.sm, s.fs, path, hb, matcher)
	results, truncated := s.collectSearchResults(resChan, cancel)

	if err := s.checkConcurrentSearchError(errChan); err != nil {
		return tools.ToolResult{}, err
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: fmt.Sprintf("0 matches found for literal string/regex '%s' in '%s'. If you are searching for a Go symbol, use 'get_definitions' or 'list_symbols' instead.", params.Query, path)}, nil
	}
	return s.formatSearchResults(results, truncated, "")
}

func (s *fileSearcher) createSearchMatcher(query string, isRegex bool) (func(string, string) (string, bool), error) {
	if isRegex {
		re, err := regexp.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w (if you intended a literal text search, set 'is_regex' to false)", err)
		}
		return func(_, line string) (string, bool) {
			return "", re.MatchString(line)
		}, nil
	}
	return func(_, line string) (string, bool) {
		return "", strings.Contains(line, query)
	}, nil
}

func (s *fileSearcher) collectSearchResults(resChan <-chan string, cancel context.CancelFunc) ([]string, bool) {
	var results []string
	limit := 100
	truncated := false
	for res := range resChan {
		if len(results) >= limit {
			truncated = true
			cancel()
			break
		}
		results = append(results, res)
	}
	return results, truncated
}

func (s *fileSearcher) grepDefinitions(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path   string `json:"path"`
		Query  string `json:"query"`
		Reason string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := s.getPathOrDefault(params.Path)
	var reQuery *regexp.Regexp
	if params.Query != "" {
		reQuery, _ = regexp.Compile("(?i)" + params.Query)
	}

	matcher := getDefinitionMatcher(reQuery, getCompiledPatterns())

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resChan, errChan := ConcurrentSearch(ctx, s.sm, s.fs, path, hb, matcher)
	results, truncated := s.collectSearchResults(resChan, cancel)

	if err := s.checkConcurrentSearchError(errChan); err != nil {
		return tools.ToolResult{}, err
	}

	return s.formatSearchResults(results, truncated, "No definitions found.")
}

func (s *fileSearcher) getPathOrDefault(path string) string {
	if path == "" {
		return "."
	}
	return path
}

func (s *fileSearcher) checkConcurrentSearchError(errChan <-chan error) error {
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func getCompiledPatterns() []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, len(defPatterns))
	for i, p := range defPatterns {
		compiled[i] = regexp.MustCompile(p)
	}
	return compiled
}

func getDefinitionMatcher(reQuery *regexp.Regexp, compiledDefs []*regexp.Regexp) func(string, string) (string, bool) {
	return func(path, line string) (string, bool) {
		if !isSupportedDefExt(filepath.Ext(path)) {
			return "", false
		}

		isDef := false
		for _, re := range compiledDefs {
			if re.MatchString(line) {
				isDef = true
				break
			}
		}

		if !isDef {
			return "", false
		}

		return "", reQuery == nil || reQuery.MatchString(line)
	}
}

func (s *fileSearcher) formatSearchResults(results []string, truncated bool, emptyMsg string) (tools.ToolResult, error) {
	if len(results) == 0 {
		return tools.ToolResult{Text: emptyMsg}, nil
	}

	out := strings.Join(results, "\n")
	if truncated {
		out += "\n... (truncated)"
	}
	return tools.ToolResult{Text: out}, nil
}

func isSupportedDefExt(ext string) bool {
	return ext == ".py" || ext == ".js" || ext == ".sh" || ext == ".md" || ext == ".go"
}
