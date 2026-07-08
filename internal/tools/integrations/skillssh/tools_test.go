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
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
	skills     []skills.Skill
	err        error
	refreshErr error
}

func (s *stubSkillRepo) GetAll(ctx context.Context) ([]skills.Skill, error) {
	return s.skills, s.err
}

func (s *stubSkillRepo) Refresh(ctx context.Context) error {
	return s.refreshErr
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

// newTestMgr creates a defaultSkillManager for testing.
// Fields left at zero value are acceptable for tests that don't exercise them.
func newTestMgr(skillsShDir string, repo skills.SkillRepository, client tools.HTTPClient, exec execRunner) *defaultSkillManager {
	return &defaultSkillManager{
		skillsShDir: skillsShDir,
		repo:        repo,
		client:      client,
		exec:        exec,
	}
}

// assertGitCloneArgs validates that capturedArgs contains the expected
// git clone arguments for installing a skill from wantURL into wantDir.
func assertGitCloneArgs(t *testing.T, args []string, wantURL, wantDir string) {
	t.Helper()
	if len(args) < 6 {
		t.Fatalf("expected at least 6 args to git clone, got %d: %v", len(args), args)
	}
	if args[0] != "clone" {
		t.Errorf("expected 'clone' subcommand, got: %s", args[0])
	}
	if args[1] != "--depth" {
		t.Errorf("expected '--depth', got: %s", args[1])
	}
	if args[2] != "1" {
		t.Errorf("expected '1', got: %s", args[2])
	}
	if args[3] != "--single-branch" {
		t.Errorf("expected '--single-branch', got: %s", args[3])
	}
	if args[4] != wantURL {
		t.Errorf("expected URL %q, got: %q", wantURL, args[4])
	}
	if args[5] != wantDir {
		t.Errorf("expected target dir %q, got: %q", wantDir, args[5])
	}
}

// --- search_skills tests ---

func TestSearchSkills_EmptyQuery(t *testing.T) {
	mgr := newTestMgr("", nil, nil, nil)
	handler := makeSearchSkills(mgr)
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
	mgr := newTestMgr("", nil, nil, nil)
	handler := makeSearchSkills(mgr)
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

	mgr := newTestMgr("", nil, client, nil)
	handler := makeSearchSkills(mgr)
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

	mgr := newTestMgr("", nil, client, nil)
	handler := makeSearchSkills(mgr)
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

	mgr := newTestMgr("", nil, client, nil)
	handler := makeSearchSkills(mgr)
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

	mgr := newTestMgr("", nil, client, nil)
	handler := makeSearchSkills(mgr)
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
	mgr := newTestMgr("", nil, nil, nil)
	handler := makeListSkills(mgr)
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
	mgr := newTestMgr("", repo, nil, nil)
	handler := makeListSkills(mgr)
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
	mgr := newTestMgr("", repo, nil, nil)
	handler := makeListSkills(mgr)
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
	mgr := newTestMgr("", repo, nil, nil)
	handler := makeListSkills(mgr)
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
	mgr := newTestMgr("", repo, nil, nil)
	handler := makeListSkills(mgr)
	res, err := handler(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, res.Text, "docs/skills/", "local", "go-patterns")
	if strings.Contains(res.Text, ".skills/") {
		t.Error("should not show .skills/ section when no skills.sh skills exist")
	}
}

func TestListSkills_NoDescription(t *testing.T) {
	repo := &stubSkillRepo{
		skills: []skills.Skill{
			{Name: "bare-skill", Description: "", Source: "local"},
			{Name: "bare-ssh", Description: "", Source: "skills.sh"},
		},
	}
	mgr := newTestMgr("", repo, nil, nil)
	handler := makeListSkills(mgr)
	res, err := handler(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := strings.Count(res.Text, "(no description)")
	if count != 2 {
		t.Errorf("expected 2 occurrences of '(no description)', got %d. Output:\n%s", count, res.Text)
	}
}

// --- install_skill tests ---

func TestInstallSkill_EmptyURL(t *testing.T) {
	mgr := newTestMgr("/tmp/skills", nil, nil, nil)
	handler := makeInstallSkill(mgr)
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
			mgr := newTestMgr("/tmp/skills", nil, nil, nil)
			handler := makeInstallSkill(mgr)
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
		url       string
		wantOwner string
		wantRepo  string
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

			mgr := newTestMgr(skillsDir, nil, nil, mockExec)
			handler := makeInstallSkill(mgr)
			res, err := handler(context.Background(), map[string]interface{}{
				"repo_url": tt.url,
			}, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(res.Text, "Successfully installed") {
				t.Errorf("expected success message, got: %s", res.Text)
			}

			expectedDir := filepath.Join(skillsDir, fmt.Sprintf("%s-%s", tt.wantOwner, tt.wantRepo))
			assertGitCloneArgs(t, capturedArgs, tt.url, expectedDir)
		})
	}
}

func TestInstallSkill_AlreadyInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")
	targetDir := filepath.Join(skillsDir, "anthropics-skills")

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	mgr := newTestMgr(skillsDir, nil, nil, nil)
	handler := makeInstallSkill(mgr)
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

	mgr := newTestMgr(skillsDir, nil, nil, mockExec)
	handler := makeInstallSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"repo_url": "https://github.com/nonexistent/repo",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "cloning repository") {
		t.Errorf("expected 'cloning repository', got: %s", res.Text)
	}
}

func TestInstallSkill_NilExec(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")

	mgr := newTestMgr(skillsDir, nil, nil, nil)
	handler := makeInstallSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"repo_url": "https://github.com/anthropics/skills",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "command execution is not available") {
		t.Errorf("expected 'command execution is not available', got: %s", res.Text)
	}
}

func TestInstallSkill_RefreshError(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")

	mockExec := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Cloning..."), nil
	}

	repo := &stubSkillRepo{
		refreshErr: errors.New("cache refresh failed"),
	}

	mgr := newTestMgr(skillsDir, repo, nil, mockExec)
	handler := makeInstallSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"repo_url": "https://github.com/anthropics/skills",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Successfully installed") {
		t.Errorf("expected 'Successfully installed', got: %s", res.Text)
	}
}

// --- remove_skill tests ---

func TestRemoveSkill_EmptyName(t *testing.T) {
	mgr := newTestMgr("/tmp/skills", nil, nil, nil)
	handler := makeRemoveSkill(mgr)
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
	mgr := newTestMgr("/tmp/skills", repo, nil, nil)
	handler := makeRemoveSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "go-patterns",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, res.Text, "Cannot remove", "local skill")
}

func TestRemoveSkill_RepoError(t *testing.T) {
	repo := &stubSkillRepo{err: errors.New("db error")}
	mgr := newTestMgr("/tmp/skills", repo, nil, nil)
	handler := makeRemoveSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "some-skill",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "db error") {
		t.Errorf("expected 'db error' in output, got: %s", res.Text)
	}
}

func TestRemoveSkill_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	mgr := newTestMgr(skillsDir, nil, nil, nil)
	handler := makeRemoveSkill(mgr)
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

	mgr := newTestMgr(skillsDir, repo, nil, nil)
	handler := makeRemoveSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "mcp-builder",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, res.Text, "Successfully removed", "mcp-builder")

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("expected skill directory to be removed")
	}
}

func TestRemoveSkill_SkillsShDirMissing(t *testing.T) {
	mgr := newTestMgr("/nonexistent/path/to/skills", nil, nil, nil)
	handler := makeRemoveSkill(mgr)
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

func TestRemoveSkill_RefreshError(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, ".skills")

	skillDir := filepath.Join(skillsDir, "test-org-test-repo", "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: test-skill\n---\nContent"),
		0644,
	); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	repo := &stubSkillRepo{
		skills: []skills.Skill{
			{Name: "test-skill", Description: "Test", Source: "skills.sh"},
		},
		refreshErr: errors.New("cache refresh failed"),
	}

	mgr := newTestMgr(skillsDir, repo, nil, nil)
	handler := makeRemoveSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": "test-skill",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Successfully removed") {
		t.Errorf("expected 'Successfully removed', got: %s", res.Text)
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
		{"SKILL.md", "SKILL.md"},
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

// --- UnmarshalArgs error path tests ---

func TestSearchSkills_BadArgs(t *testing.T) {
	mgr := newTestMgr("", nil, nil, nil)
	handler := makeSearchSkills(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"query": 123, // int, not string
	}, nil)
	if err != nil {
		t.Fatalf("handler must never return error: %v", err)
	}
	if res.Error == nil {
		t.Error("expected res.Error to be non-nil for bad args")
	}
	if !strings.Contains(res.Text, "Error:") {
		t.Errorf("expected 'Error:' in Text, got: %s", res.Text)
	}
}

func TestInstallSkill_BadArgs(t *testing.T) {
	mgr := newTestMgr("/tmp/skills", nil, nil, nil)
	handler := makeInstallSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"repo_url": 456, // int, not string
	}, nil)
	if err != nil {
		t.Fatalf("handler must never return error: %v", err)
	}
	if res.Error == nil {
		t.Error("expected res.Error to be non-nil for bad args")
	}
	if !strings.Contains(res.Text, "Error:") {
		t.Errorf("expected 'Error:' in Text, got: %s", res.Text)
	}
}

func TestRemoveSkill_BadArgs(t *testing.T) {
	mgr := newTestMgr("/tmp/skills", nil, nil, nil)
	handler := makeRemoveSkill(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"name": 789, // int, not string
	}, nil)
	if err != nil {
		t.Fatalf("handler must never return error: %v", err)
	}
	if res.Error == nil {
		t.Error("expected res.Error to be non-nil for bad args")
	}
	if !strings.Contains(res.Text, "Error:") {
		t.Errorf("expected 'Error:' in Text, got: %s", res.Text)
	}
}

// --- searchGitHubAPI error path tests ---

func TestSearchSkills_HTTPErrorStatus(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return makeStringResponse(403, `{"message":"API rate limit exceeded"}`), nil
		},
	}

	mgr := newTestMgr("", nil, client, nil)
	handler := makeSearchSkills(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"query": "test",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "GitHub API error") && !strings.Contains(res.Text, "searching skills") {
		t.Errorf("expected API error in output, got: %s", res.Text)
	}
}

func TestSearchSkills_BadJSON(t *testing.T) {
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return makeStringResponse(200, `this is not json`), nil
		},
	}

	mgr := newTestMgr("", nil, client, nil)
	handler := makeSearchSkills(mgr)
	res, err := handler(context.Background(), map[string]interface{}{
		"query": "test",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "parse response") && !strings.Contains(res.Text, "searching skills") {
		t.Errorf("expected parse error in output, got: %s", res.Text)
	}
}

func TestSearchSkills_WithToken(t *testing.T) {
	var capturedAuth string
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			capturedAuth = req.Header.Get("Authorization")
			return makeStringResponse(200, `{"total_count":0,"items":[]}`), nil
		},
	}

	mgr := NewSkillManager("/tmp/.skills", nil, client, nil, "ghp_testtoken123")
	handler := makeSearchSkills(mgr)
	_, err := handler(context.Background(), map[string]interface{}{
		"query": "test",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedAuth != "Bearer ghp_testtoken123" {
		t.Errorf("expected Authorization header 'Bearer ghp_testtoken123', got: %q", capturedAuth)
	}
}
