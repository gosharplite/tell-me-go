// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/integrations/ado"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/atlassian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type ioErrorReader struct{}

func (e *ioErrorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func (e *ioErrorReader) Close() error {
	return nil
}

type mockHttpClient struct {
	mock.Mock
}

func (m *mockHttpClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestAzureDevOps_IOError(t *testing.T) {
	mockClient := new(mockHttpClient)
	m := ado.NewADOManager(nil, ado.WithHTTPClient(mockClient), ado.WithToken("test-pat"))

	mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool { return true })).Return(&http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Body:       &ioErrorReader{},
	}, nil)

	err := m.CheckResponseError(&http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Body:       &ioErrorReader{},
	}, "http://url")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read response body")
}

func TestJira_IOError(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "mock-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://jira.com")
	mockClient := new(mockHttpClient)
	m, err := atlassian.NewJiraManager(nil, mockClient)
	assert.NoError(t, err)

	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Body:       &ioErrorReader{},
	}, nil)

	ctx := context.Background()

	// Test jiraSearchIssues
	_, err = m.JiraSearchIssues(ctx, map[string]interface{}{"jql": "project=PROJ"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read response body")

	// Test jiraGetIssue
	_, err = m.JiraGetIssue(ctx, map[string]interface{}{"issue_key": "PROJ-1"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read response body")
}

func TestConfluence_IOError(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "mock-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://confluence.com")
	mockClient := new(mockHttpClient)
	m, err := atlassian.NewConfluenceManager(nil, mockClient)
	assert.NoError(t, err)

	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Body:       &ioErrorReader{},
	}, nil)

	ctx := context.Background()

	// Test fetchSearchPage
	_, err = m.FetchSearchPage(ctx, "https://confluence.com/api")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read response body")
}
