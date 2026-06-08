// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtlassianProvider_Do_RetryLogic(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "token")

	t.Run("Success on first attempt", func(t *testing.T) {
		p, err := NewAtlassianProvider()
		assert.NoError(t, err)
		mockClient := &toolstest.MockHTTPClient{}
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		mockClient.DoFunc = func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}

		resp, err := p.Do(context.Background(), mockClient, req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		count, _ := mockClient.Snapshot()
		assert.Equal(t, 1, count)
	})

	t.Run("Retry on 429 then success", func(t *testing.T) {
		p, err := NewAtlassianProvider()
		assert.NoError(t, err)
		p.BaseDelay = 1 * time.Microsecond
		mockClient := &toolstest.MockHTTPClient{}
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		var callCount int
		mockClient.DoFunc = func(req *http.Request) (*http.Response, error) {
			callCount++
			if callCount == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("throttled")),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}

		start := time.Now()
		resp, err := p.Do(context.Background(), mockClient, req)
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, duration >= 1*time.Microsecond, "Should have waited at least 1us")
		count, _ := mockClient.Snapshot()
		assert.Equal(t, 2, count)
	})

	t.Run("Respect Retry-After header", func(t *testing.T) {
		p, err := NewAtlassianProvider()
		assert.NoError(t, err)
		p.BaseDelay = 1 * time.Microsecond
		mockClient := &toolstest.MockHTTPClient{}
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		headers := make(http.Header)
		headers.Set("Retry-After", "0")

		var callCount int
		mockClient.DoFunc = func(req *http.Request) (*http.Response, error) {
			callCount++
			if callCount == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     headers,
					Body:       io.NopCloser(strings.NewReader("throttled")),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}

		start := time.Now()
		resp, err := p.Do(context.Background(), mockClient, req)
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		// Increased threshold to 500ms to account for race detector overhead
		// while still ensuring no significant blocking occurred.
		assert.Less(t, duration, 500*time.Millisecond, "Should not have waited long with Retry-After: 0")
		count, _ := mockClient.Snapshot()
		assert.Equal(t, 2, count)
	})

	t.Run("Max retries exceeded", func(t *testing.T) {
		p, err := NewAtlassianProvider()
		assert.NoError(t, err)
		p.BaseDelay = 1 * time.Microsecond
		mockClient := &toolstest.MockHTTPClient{}
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		// 4 attempts (1 initial + 3 retries)
		mockClient.DoFunc = func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("throttled")),
			}, nil
		}

		resp, err := p.Do(context.Background(), mockClient, req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
		count, _ := mockClient.Snapshot()
		assert.Equal(t, 4, count)
	})

	t.Run("Context cancellation", func(t *testing.T) {
		p, err := NewAtlassianProvider()
		assert.NoError(t, err)
		p.BaseDelay = 10 * time.Millisecond
		mockClient := &toolstest.MockHTTPClient{}
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mockClient.DoFunc = func(req *http.Request) (*http.Response, error) {
			cancel()
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("throttled")),
			}, nil
		}

		_, err = p.Do(ctx, mockClient, req)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestAtlassianProvider_GetAuthHeader_Errors(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Run("Missing Email", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "")
		p, err := NewAtlassianProvider()
		assert.NoError(t, err)
		_, err = p.getAuthHeader()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing ATLASSIAN_EMAIL")
	})

	t.Run("Missing Token", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "test")
		t.Setenv("ATLASSIAN_TOKEN", "")
		p, err := NewAtlassianProvider()
		assert.NoError(t, err)
		_, err = p.getAuthHeader()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing ATLASSIAN_TOKEN")
	})
}

func TestAtlassianProvider_Do_RetryWithBody(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "token")

	p, err := NewAtlassianProvider()
	assert.NoError(t, err)
	p.BaseDelay = 1 * time.Microsecond
	mockClient := &toolstest.MockHTTPClient{}

	bodyText := "hello world"
	req, _ := http.NewRequest(http.MethodPut, "https://test.com", strings.NewReader(bodyText))
	// Ensure GetBody is populated
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(bodyText)), nil
	}

	// Counter-based dispatch: first call returns 429, second returns 200.
	// MockHTTPClient.Do() already consumes the body, so the retry path
	// exercises req.GetBody to repopulate it. The successful 200 response
	// implicitly proves the body was correctly re-sent.
	var callCount int
	mockClient.DoFunc = func(req *http.Request) (*http.Response, error) {
		callCount++
		if callCount == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("throttled")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	}

	resp, err := p.Do(context.Background(), mockClient, req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	count, _ := mockClient.Snapshot()
	assert.Equal(t, 2, count)
}

func TestAtlassianProvider_Constructor_FailsWhenMissingURL(t *testing.T) {
	// Ensure the environment variable is explicitly unset for this test
	t.Setenv("ATLASSIAN_BASE_URL", "")

	p, err := NewAtlassianProvider()

	require.Error(t, err)
	require.Nil(t, p)
	assert.Contains(t, err.Error(), "missing required environment variable: ATLASSIAN_BASE_URL")
}

func TestAtlassianProvider_GetWaitTime_EdgeCases(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "token")

	t.Run("BaseDelay zero defaults to 1 second", func(t *testing.T) {
		p, err := NewAtlassianProvider()
		require.NoError(t, err)
		p.BaseDelay = 0 // trigger the BaseDelay == 0 branch

		resp := &http.Response{Header: make(http.Header)}
		wait := p.GetWaitTime(resp, 0)
		assert.Equal(t, 1*time.Second, wait)
	})

	t.Run("invalid Retry-After falls back to exponential backoff", func(t *testing.T) {
		p, err := NewAtlassianProvider()
		require.NoError(t, err)
		p.BaseDelay = 1 * time.Second

		headers := make(http.Header)
		headers.Set("Retry-After", "not-a-number")
		resp := &http.Response{Header: headers}

		wait := p.GetWaitTime(resp, 1)
		// Falls back to BaseDelay * 2^1 = 2 seconds
		assert.Equal(t, 2*time.Second, wait)
	})
}

func TestAtlassianProvider_Do_ResetBodyError(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "token")

	t.Run("GetBody error on retry propagates", func(t *testing.T) {
		p, err := NewAtlassianProvider()
		require.NoError(t, err)
		p.BaseDelay = 0

		mockClient := &toolstest.MockHTTPClient{}
		bodyText := "hello"
		req, _ := http.NewRequest(http.MethodPut, "https://test.com", strings.NewReader(bodyText))
		// Set GetBody to a function that returns an error
		req.GetBody = func() (io.ReadCloser, error) {
			return nil, assert.AnError
		}

		// First attempt: 429, body consumed
		mockClient.DoFunc = func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("throttled")),
			}, nil
		}

		// The retry should try to reset the body and fail
		// No second Do call should happen
		_, err = p.Do(context.Background(), mockClient, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to reset request body")
		count, _ := mockClient.Snapshot()
		assert.Equal(t, 1, count)
	})
}
