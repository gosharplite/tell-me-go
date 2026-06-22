// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfluenceManager_FetchSpaceByKey_NewRequestError(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	m, err := NewConfluenceManager(nil, nil)
	require.NoError(t, err)
	m.provider.baseURL = "://invalid-scheme"

	_, err = m.fetchSpaceByKey(context.Background(), "SPACE")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base url")
}
