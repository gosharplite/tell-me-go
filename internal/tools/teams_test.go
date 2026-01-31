// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSendTeamsMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sm := NewSecurityManager()
	m := &teamsManager{sm: sm}

	// Mock confirmation
	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")

	args := map[string]interface{}{
		"webhook_url": server.URL,
		"message":     "Hello Teams!",
	}

	res, err := m.sendTeamsMessage(context.Background(), args)
	if err != nil {
		t.Fatalf("sendTeamsMessage failed: %v", err)
	}

	if res.Text != "Successfully sent message to Teams. Status: 200 OK" {
		t.Errorf("Unexpected result: %s", res.Text)
	}
}
