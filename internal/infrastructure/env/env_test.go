// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package env

import (
	"errors"
	"testing"
)

func TestResolveHomeDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		envVars    map[string]string
		homeDir    string
		homeDirErr error
		expected   string
	}{
		{
			name:     "TELL_ME_HOME takes precedence",
			envVars:  map[string]string{"TELL_ME_HOME": "/custom/tellme", "AIT_HOME": "/custom/ait"},
			expected: "/custom/tellme",
		},
		{
			name:     "AIT_HOME used if TELL_ME_HOME is empty",
			envVars:  map[string]string{"TELL_ME_HOME": "", "AIT_HOME": "/custom/ait"},
			expected: "/custom/ait",
		},
		{
			name:     "Fallback to user home dir",
			envVars:  map[string]string{},
			homeDir:  "/home/user",
			expected: "/home/user",
		},
		{
			name:       "Fallback to dot on user home dir error",
			envVars:    map[string]string{},
			homeDirErr: errors.New("simulated OS error"),
			expected:   ".",
		},
		{
			name:     "Fallback to dot on empty user home dir",
			envVars:  map[string]string{},
			homeDir:  "",
			expected: ".",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockGetEnv := func(k string) string {
				return tt.envVars[k]
			}
			mockUserHomeDir := func() (string, error) {
				return tt.homeDir, tt.homeDirErr
			}

			got := ResolveHomeDir(mockGetEnv, mockUserHomeDir)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
