// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import "context"

// SkillManager encapsulates all skills.sh business logic.
// Tool handlers use this interface so that transport (arg unmarshaling,
// result formatting) is separated from service logic (HTTP, git, filesystem).
type SkillManager interface {
	SearchSkills(ctx context.Context, query string) (string, error)
	InstallSkill(ctx context.Context, repoURL string) (string, error)
	ListSkills(ctx context.Context) (string, error)
	RemoveSkill(ctx context.Context, name string) (string, error)
}
