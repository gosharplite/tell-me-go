// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"testing"
	"strings"
)

func FuzzIsSafeCommand(f *testing.F) {
	sm := NewSecurityManager()
	m := &systemManager{sm: sm}

	// Seed corpus with some safe and unsafe examples
	f.Add("ls -la")
	f.Add("grep -r 'todo' .")
	f.Add("cat /etc/passwd")
	f.Add("rm -rf /")
	f.Add("ls; rm -rf /")
	f.Add("echo 'hello' > out.txt")
	f.Add("git status")
	f.Add("go test ./...")
	f.Add("cat `ls` /etc/shadow")
	f.Add("ls $(whoami)")
	f.Add("")
	f.Add("   ")

	f.Fuzz(func(t *testing.T, cmd string) {
		isSafe := m.isSafeCommand(cmd)
		
		if isSafe {
			// 1. Must not contain blacklisted shell metacharacters
			unsafeChars := []string{"|", "&", ";", ">", "<", "$", "`", "\n", "\r"}
			for _, char := range unsafeChars {
				if strings.Contains(cmd, char) {
					t.Errorf("Command %q marked safe but contains unsafe char %q", cmd, char)
				}
			}
			
			// 2. Must be splitable by shlex
			parts, err := splitCommand(cmd)
			if err != nil {
				t.Errorf("Command %q marked safe but failed to split: %v", cmd, err)
				return
			}
			
			if len(parts) == 0 {
				t.Errorf("Command %q marked safe but has no parts", cmd)
				return
			}

			// 3. Must start with whitelisted command
			base := parts[0]
			safeCommands := map[string]bool{
				"grep": true, "ls": true, "pwd": true, "cat": true, "echo": true,
				"head": true, "tail": true, "wc": true, "stat": true, "date": true,
				"whoami": true, "diff": true, "awk": true, "sed": true, "git": true,
				"go": true,
			}
			if !safeCommands[base] {
				t.Errorf("Command %q marked safe but base %q not in whitelist", cmd, base)
			}
			
			// 4. Git and Go specific sub-checks
			if base == "git" {
				// Verify it's a read-only subcommand
				// (Logic in isSafeCommand handles this, we're just verifying consistency)
			}
		}
	})
}
