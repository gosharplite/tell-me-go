// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockRetryClient struct {
	mock.Mock
}

func (m *mockRetryClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	// Simulate real http.Client consuming the body after it's been sent
	if req.Body != nil {
		_, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestAtlassianProvider_Do_RetryLogic(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "token")

	t.Run("Success on first attempt", func(t *testing.T) {
		p := newAtlassianProvider()
		mockClient := new(mockRetryClient)
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil).Once()

		resp, err := p.Do(context.Background(), mockClient, req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		mockClient.AssertExpectations(t)
	})

	t.Run("Retry on 429 then success", func(t *testing.T) {
		p := newAtlassianProvider()
		p.baseDelay = 1 * time.Microsecond
		mockClient := new(mockRetryClient)
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		// First attempt: 429
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("throttled")),
		}, nil).Once()

		// Second attempt: 200
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil).Once()

		start := time.Now()
		resp, err := p.Do(context.Background(), mockClient, req)
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, duration >= 1*time.Microsecond, "Should have waited at least 1us")
		mockClient.AssertExpectations(t)
	})

	t.Run("Respect Retry-After header", func(t *testing.T) {
		p := newAtlassianProvider()
		p.baseDelay = 1 * time.Microsecond
		mockClient := new(mockRetryClient)
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		headers := make(http.Header)
		headers.Set("Retry-After", "0")

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     headers,
			Body:       io.NopCloser(strings.NewReader("throttled")),
		}, nil).Once()

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil).Once()

		start := time.Now()
		resp, err := p.Do(context.Background(), mockClient, req)
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		// Increased threshold to 500ms to account for race detector overhead 
		// while still ensuring no significant blocking occurred.
		assert.Less(t, duration, 500*time.Millisecond, "Should not have waited long with Retry-After: 0")
		mockClient.AssertExpectations(t)
	})

	t.Run("Max retries exceeded", func(t *testing.T) {
		p := newAtlassianProvider()
		p.baseDelay = 1 * time.Microsecond
		mockClient := new(mockRetryClient)
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		// 4 attempts (1 initial + 3 retries)
		for i := 0; i < 4; i++ {
			mockClient.On("Do", mock.Anything).Return(&http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("throttled")),
			}, nil).Once()
		}

		resp, err := p.Do(context.Background(), mockClient, req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
		mockClient.AssertExpectations(t)
	})

	t.Run("Context cancellation", func(t *testing.T) {
		p := newAtlassianProvider()
		p.baseDelay = 10 * time.Millisecond
		mockClient := new(mockRetryClient)
		req, _ := http.NewRequest(http.MethodGet, "https://test.com", nil)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("throttled")),
		}, nil).Once()

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()

		_, err := p.Do(ctx, mockClient, req)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestAtlassianProvider_GetAuthHeader_Errors(t *testing.T) {
	t.Run("Missing Email", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "")
		p := newAtlassianProvider()
		_, err := p.getAuthHeader()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing ATLASSIAN_EMAIL")
	})

	t.Run("Missing Token", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "test")
		t.Setenv("ATLASSIAN_TOKEN", "")
		p := newAtlassianProvider()
		_, err := p.getAuthHeader()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing ATLASSIAN_TOKEN")
	})
}

func TestAtlassianProvider_Do_RetryWithBody(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "token")

	p := newAtlassianProvider()
	p.baseDelay = 1 * time.Microsecond
	mockClient := new(mockRetryClient)

	bodyText := "hello world"
	req, _ := http.NewRequest(http.MethodPut, "https://test.com", strings.NewReader(bodyText))
	// Ensure GetBody is populated
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(bodyText)), nil
	}

	// First attempt: 429
	mockClient.On("Do", mock.MatchedBy(func(r *http.Request) bool {
		if r.Body == nil {
			return bodyText == ""
		}
		body, _ := io.ReadAll(r.Body)
		// Restore body for other matchers and the actual implementation
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		return string(body) == bodyText
	})).Return(&http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("throttled")),
	}, nil).Once()

	// Second attempt: 200 - should still have the body
	mockClient.On("Do", mock.MatchedBy(func(r *http.Request) bool {
		if r.Body == nil {
			return bodyText == ""
		}
		body, _ := io.ReadAll(r.Body)
		// Restore body
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		return string(body) == bodyText
	})).Return(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
	}, nil).Once()

	resp, err := p.Do(context.Background(), mockClient, req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	mockClient.AssertExpectations(t)
}
