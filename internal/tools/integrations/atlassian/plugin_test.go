// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
	"github.com/stretchr/testify/assert"
)

func TestAtlassianPlugin_Register(t *testing.T) {
	t.Run("RegisterConfluence failure wraps error with confluence prefix", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}
		confluenceErr := errors.New("confluence boom")

		// Fail on the 1st RegisterToToolkit call (confluence_search).
		reg.registerToToolkitErrProvider = func(callNum int) error {
			if callNum == 1 {
				return confluenceErr
			}
			return nil
		}

		p := NewPlugin()
		deps := plugin.PluginDependencies{SecurityMgr: sm, HTTPClient: nil}
		err := p.Register(reg, deps)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "confluence:")
		assert.ErrorIs(t, err, confluenceErr)
	})

	t.Run("RegisterJira failure wraps error with jira prefix", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}
		jiraErr := errors.New("jira boom")

		// Allow Confluence (RegisterToToolkit calls 1-2 +
		// RegisterToToolkitWithOptions call 1) to succeed, fail on
		// RegisterToToolkit call 4 (jira_get_issue).
		reg.registerToToolkitErrProvider = func(callNum int) error {
			if callNum == 4 {
				return jiraErr
			}
			return nil
		}

		p := NewPlugin()
		deps := plugin.PluginDependencies{SecurityMgr: sm, HTTPClient: nil}
		err := p.Register(reg, deps)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "jira:")
		assert.ErrorIs(t, err, jiraErr)
	})
}
