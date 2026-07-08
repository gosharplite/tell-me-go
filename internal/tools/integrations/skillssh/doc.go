// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package skillssh provides tools for interacting with the skills.sh ecosystem:
// searching for installable skills, listing installed skills, installing new
// skills via git clone, and removing skills.
//
// Skills are stored as SKILL.md files (YAML frontmatter + Markdown body) in
// the $TELL_ME_HOME/.skills/ directory, organized by owner-repo.
package skillssh
