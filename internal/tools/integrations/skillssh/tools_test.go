// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// --- Mock types ---

// mockHTTPClient implements tools.HTTPClient for testing.
type mockHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

// stubSkillRepo implements skills.SkillRepository for testing.
type stubSkillRepo struct {
	skills []skills.Skill
	err    error
}

func (s *stubSkillRepo) GetAll(ctx context.Context) ([]skills.Skill, error) {
	return s.skills, s.err
}

// --- Helpers ---

// makeStringResponse creates an HTTP response with the given status and body.
func makeStringResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// assertContains checks that text contains all substrings.
func assertContains(t *testing.T, text string, substrs ...string) {
	t.Helper()
	for _, s := range substrs {
		if !strings.Contains(text, s) {
			t.Errorf("expected output to contain %q, got:\n%s", s, text)
		}
	}
}

// --- search_skills tests ---

func TestSearchSkills_EmptyQuery(t *testing.T) {
	handler := makeSearchSkills(nil)
	res, err := handler(context.Background(), map[string]interface{}{
		"query": "",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "query is required") {
		t.Errorf("expected 'query is required', got: %s", res.Text)
	}
}

func TestSearchSkills_MissingQuery(t *testing.T) {
	handler := makeSearchSkills(nil)
	res, err := handler(context.Background(), map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "query is required") {
		t.Errorf("expected 'query is required', got: %s", res.Text)
	}
}

func TestSearchSkills_URLConstruction(t *testing.T) {
	var capturedURL string
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			capturedURL = req.URL.String()
			return makeStringResponse(200, `{"total_count":0,"items":[]}`), nil
		},
	}

	handler := makeSearchSkills(client)
	_, err := handler(context.Background(), map[string]interface{}{
		"query": "kubernetes",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedURL, "SKILL.md") {
		t.Errorf("URL should contain SKILL.md, got: %s", capturedURL)
	}
	if !strings.Contains(capturedURL, "kubernetes") {
		t.Errorf("URL should contain kubernetes, got: %s", capturedURL)
	}
	if !strings.Contains(capturedURL, "in:file") {
		t.Errorf("URL should contain in:file, got: %s", capturedURL)
	}
	if !strings.Contains(capturedURL, "path:skills") {
		t.Errorf("URL should contain path:skills, got: %s", capturedURL)
	}
}

func TestSearchSkills_NoResults(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return makeStringResponse(200, `{"total_count":0,"items":[]}`), nil
		},
	}

	handler := makeSearchSkills(client)
	res, err := handler(context.Background(), map[string]interface{}{
		"query": "nonexistent",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "No skills found") {
		t.Errorf("expected 'No skills found', got: %s", res.Text)
	}
}

func TestSearchSkills_HTTPError(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		},
	}

	handler := makeSearchSkills(client)
	res, err := handler(context.Background(), map[string]interface{}{
		"query": "test",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "connection refused") {
		t.Errorf("expected connection error in output, got: %s", res.Text)
	}
}

func TestSearchSkills_ResultsParsed(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			// The first request is the search, the second is fetching SKILL.md
			if strings.Contains(req.URL.String(), "raw.githubusercontent.com") {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader("---\nname: mcp-builder\ndescription: Create MCP servers\n---\nContent here")),
				}, nil
			}
			return makeStringResponse(200, `{
				"total_count":1,
				"items":[{
					"name":"SKILL.md",
					"path":"skills/mcp-builder/SKILL.md",
					"repository":{"full_name":"anthropics/skills"}
				}]
			}`), nil
		},
	}

	handler := makeSearchSkills(client)
	res, err := handler(context.Background(), map[string]interface{}{
		"query": "mcp",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, res.Text, "mcp-builder", "Create MCP servers", "anthropics/skills", "install_skill")
}

// --- list_skills tests ---

func TestListSkills_NilRepo(t *testing.T) {
	handler := makeListSkills(nil)
	res, err := handler(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "No skills repository") {
		t.Errorf("expected 'No skills repository', got: %s", res.Text)
	}
}

func TestListSkills_Empty(t *testing.T) {
	repo := &stubSkillRepo{skills: []skills.Skill{}}
	handler := makeListSkills(repo)
	res, err := handler(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "No skills installed") {
		t.Errorf("expected 'No skills installed', got: %s", res.Text)
	}
	if !strings.Contains(res.Text, "search_skills") {
		t.Errorf("expected mention of search_skills, got: %s", res.Text)
	}
}

func TestListSkills_RepoError(t *testing.T) {
	repo := &stubSkillRepo{err: errors.New("db error")}
	handler := makeListSkills(repo)
	res, err := handler(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "db error") {
		t.Errorf("expected 'db error' in output, got: %s", res.Text)
	}
}

func TestListSkills_BothSources(t *testing.T) {
	repo := &stubSkillRepo{
		skills: []skills.Skill{
			{Name: "go-patterns", Description: "Go patterns", Source: "local"},
			{Name: "go-testing", Description: "Go testing", Source: "local"},
			{Name: "mcp-builder", Description: "MCP servers", Source: "skills.sh"},
			{Name: "k8s-tools", Description: "K8s tools", Source: "skills.sh"},
		},
	}
	handler := makeListSkills(repo)
	res, err := handler(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, res.Text,
		"docs/skills/",
		"local",
		"go-patterns",
		"go-testing",
		".skills/",
		"skills.sh",
		"mcp-builder",
		"k8s-tools",
	)
}

func TestListSkills_LocalOnly(t *testing.T) {
	repo := &stubSkillRepo{
		skills: []skills.Skill{
			{Name: "go-patterns", Description: "Go patterns", Source: "local"},
		},
	}
	handler := makeListSkills(repo)
	res, err := handler(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, res.Text, "docs/skills/", "local", "go-patterns")
	if strings.Contains(res.Text, ".skills/") {
		t.Error("should not show .skills/ section when no skills.sh skills exist")
	}
}

// --- install_skill tests ---

func TestInstallSkill_EmptyURL(t *testing.T) {
	handler := makeInstallSkill("/tmp/skills", nil)
	res, err := handler(context.Background(), map[string]interface{}{
		"repo_url": "",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "repo_url is required") {
		t.Errorf("expected 'repo_url is required', got: %s", res.Text)
	}
}

func TestInstallSkill_InvalidURLs(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"not-a-url", "invalid GitHub repository URL"},
		{"https://gitlab.com/foo/bar", "invalid GitHub repository URL"},
		{"https://github.com/owner", "invalid GitHub repository URL"},
		{"ftp://github.com/owner/repo", "invalid GitHub repository URL"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("reject_%s", tt.url), func(t *testing.T) {
			handler := makeInstallSkill("/tmp/skills", nil)
			res, err := handler(context.Background(), map[string]interface{}{
				"repo_url": tt.url,
			}, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(res.Text, tt.want) {
				t.Errorf("expected %q in output, got: %s", tt.want, res.Text)
			}
		})
	}
}

func TestInstallSkill_ValidURLs(t *testing.T) {
	tests := []struct {
		url          string
		wantOwner    string
		wantRepo     string
	}{
		{"https://github.com/anthropics/skills", "anthropics", "skills"},
		{"https://github.com/anthropics/skills.git", "anthropics", "skills"},
		{"https://github.com/anthropics/skills/", "anthropics", "skills"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("accept_%s", tt.url), func(t *testing.T) {
			tmpDir := t.TempDir()
			skillsDir := filepath.Join(tmpDir, ".skills")

			var capturedArgs []string
			mockExec := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				capturedArgs = args
				return []byte("Cloning..."), nil
			}

			handler := makeInstallSkill(skillsDir, mockExec)
			res, err := handler(context.Background(), map[string]interface{}{
				"repo_url": tt.url,
			}, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(res.Text, "Successfully installed") {
				t.Errorf("expected success message, got: %s", res.Text)
			}

			// Verify git clone was called with correct args
			if len(capturedArgs) < 3 {
				t.Fatalf("expected at least 3 args to git clone, got %d: %v", len(capturedArgs), capturedArgs)
			}
			if capturedArgs[0] != "clone" {
				t.Errorf("expected 'clone' subcommand, got: %s", capturedArgs[0])
			}
			if capturedArgs[1] != tt.url {
				t.Errorf("expected URL %q, got: %q", tt.url, capturedArgs[1])
			}
			expectedDir := filepath.Join(skillsDir, fmt.Sprintf("%s-%s", tt.wantOwner, tt.wantRepo))
			if capturedArgs[2] != expectedDir {
				t.Errorf("expected target dir %q, got: %q", expectedDir, capturedArgs[2])
			}
		})
	}
}

func TestInstallSkill_AlreadyInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")
	targetDir := filepath.Join(skillsDir, "anthropics-skills")

	// Pre-create the target directory
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	handler := makeInstallSkill(skillsDir, nil)
	res, err := handler(context.Background(), map[string]interface{}{
		"repo_url": "https://github.com/anthropics/skills",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "already installed") {
		t.Errorf("expected 'already installed', got: %s", res.Text)
	}
}

func TestInstallSkill_ExecFails(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")

	mockExec := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("fatal: repository not found"), errors.New("exit status 128")
	}

	handler := makeInstallSkill(skillsDir, mockExec)
	res, err := handler(context.Background(), map[string]interface{}{
		"repo_url": "https://github.com/nonexistent/repo",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Error cloning") {
		t.Errorf("expected 'Error cloning', got: %s", res.Text)
	}
}

// --- remove_skill tests ---

func TestRemoveSkill_EmptyName(t *testing.T) {
	handler := makeRemoveSkill("/tmp/skills", nil)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "name is required") {
		t.Errorf("expected 'name is required', got: %s", res.Text)
	}
}

func TestRemoveSkill_LocalSkillRejected(t *testing.T) {
	repo := &stubSkillRepo{
		skills: []skills.Skill{
			{Name: "go-patterns", Description: "Go patterns", Source: "local"},
		},
	}
	handler := makeRemoveSkill("/tmp/skills", repo)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "go-patterns",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, res.Text, "Cannot remove", "local skill")
}

func TestRemoveSkill_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")
	// Create empty .skills/ dir
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	handler := makeRemoveSkill(skillsDir, nil)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "nonexistent-skill",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "not found") {
		t.Errorf("expected 'not found', got: %s", res.Text)
	}
}

func TestRemoveSkill_RemovesSkillShSkill(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")

	// Create a nested skill directory structure
	skillDir := filepath.Join(skillsDir, "anthropics-skills", "skills", "mcp-builder")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: mcp-builder\ndescription: Create MCP servers\n---\nContent"),
		0644,
	); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	repo := &stubSkillRepo{
		skills: []skills.Skill{
			{Name: "mcp-builder", Description: "MCP servers", Source: "skills.sh"},
		},
	}

	handler := makeRemoveSkill(skillsDir, repo)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "mcp-builder",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, res.Text, "Successfully removed", "mcp-builder")

	// Verify the directory was actually removed
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("expected skill directory to be removed")
	}
}

func TestRemoveSkill_SkillsShDirMissing(t *testing.T) {
	handler := makeRemoveSkill("/nonexistent/path/to/skills", nil)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "some-skill",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "not found") {
		t.Errorf("expected 'not found' for missing dir, got: %s", res.Text)
	}
}

// --- deriveSkillName tests ---

func TestDeriveSkillName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"skills/mcp-builder/SKILL.md", "mcp-builder"},
		{"owner-repo/skills/go-patterns/SKILL.md", "go-patterns"},
		{"deep/nested/skills/k8s/SKILL.md", "k8s"},
		{"SKILL.md", "SKILL.md"}, // fallback
		{"no-skills-dir/SKILL.md", "no-skills-dir"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := deriveSkillName(tt.path)
			if got != tt.want {
				t.Errorf("deriveSkillName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// --- ghRepoURL regex tests ---

func TestGhRepoURL(t *testing.T) {
	tests := []struct {
		url   string
		match bool
		owner string
		repo  string
	}{
		{"https://github.com/anthropics/skills", true, "anthropics", "skills"},
		{"https://github.com/anthropics/skills.git", true, "anthropics", "skills"},
		{"https://github.com/anthropics/skills/", true, "anthropics", "skills"},
		{"https://github.com/a/b", true, "a", "b"},
		{"https://gitlab.com/foo/bar", false, "", ""},
		{"http://github.com/foo/bar", false, "", ""},
		{"https://github.com/owner", false, "", ""},
		{"https://github.com/", false, "", ""},
		{"not-a-url", false, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			matches := ghRepoURL.FindStringSubmatch(tt.url)
			if tt.match {
				if matches == nil {
					t.Errorf("expected match for %q", tt.url)
				} else {
					if matches[1] != tt.owner {
						t.Errorf("owner = %q, want %q", matches[1], tt.owner)
					}
					if matches[2] != tt.repo {
						t.Errorf("repo = %q, want %q", matches[2], tt.repo)
					}
				}
			} else {
				if matches != nil {
					t.Errorf("expected no match for %q, got %v", tt.url, matches)
				}
			}
		})
	}
}

// --- parseSkillFrontmatter tests ---

func TestParseSkillFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantDesc string
	}{
		{
			name:     "valid frontmatter",
			input:    "---\nname: my-skill\ndescription: My description\n---\nContent",
			wantName: "my-skill",
			wantDesc: "My description",
		},
		{
			name:     "no frontmatter",
			input:    "Just some markdown",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "missing closing",
			input:    "---\nname: test\n",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "name only",
			input:    "---\nname: only-name\n---\nContent",
			wantName: "only-name",
			wantDesc: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotDesc := parseSkillFrontmatter([]byte(tt.input))
			if gotName != tt.wantName {
				t.Errorf("name = %q, want %q", gotName, tt.wantName)
			}
			if gotDesc != tt.wantDesc {
				t.Errorf("desc = %q, want %q", gotDesc, tt.wantDesc)
			}
		})
	}
}
