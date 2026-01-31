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
	f.Add("rm -rf /")
	f.Add("go run main.go")
	f.Add("sed -i 's/a/b/g' file.txt")
	f.Add("awk '{print $1}' /etc/shadow")

	f.Fuzz(func(t *testing.T, cmd string) {
		isSafe := m.isSafeCommand(cmd)
		
		// 0. Negative assertions: Known unsafe patterns must be rejected
		unsafeChars := []string{"|", "&", ";", ">", "<", "$", "`", "\n", "\r"}
		for _, char := range unsafeChars {
			if strings.Contains(cmd, char) {
				if isSafe {
					t.Errorf("Command %q marked safe but contains unsafe char %q", cmd, char)
				}
				return // Already checked one unsafe condition
			}
		}

		if isSafe {
			// 1. Must be splitable by shlex
			parts, err := splitCommand(cmd)
			if err != nil {
				t.Errorf("Command %q marked safe but failed to split: %v", cmd, err)
				return
			}
			
			if len(parts) == 0 {
				t.Errorf("Command %q marked safe but has no parts", cmd)
				return
			}

			// 2. Must start with whitelisted command
			base := parts[0]
			safeCommands := map[string]bool{
				"grep": true, "ls": true, "pwd": true, "cat": true, "echo": true,
				"head": true, "tail": true, "wc": true, "stat": true, "date": true,
				"whoami": true, "diff": true, "git": true, "go": true,
			}
			if !safeCommands[base] {
				t.Errorf("Command %q marked safe but base %q not in whitelist", cmd, base)
			}
			
			// 3. Go specific sub-checks: run/build/install/mod must be rejected
			if base == "go" {
				sub := ""
				for i := 1; i < len(parts); i++ {
					if !strings.HasPrefix(parts[i], "-") {
						sub = parts[i]
						break
					}
				}
				unsafeGo := map[string]bool{"run": true, "build": true, "install": true, "get": true, "mod": true}
				if unsafeGo[sub] {
					t.Errorf("Command %q marked safe but uses unsafe 'go %s' subcommand", cmd, sub)
				}
			}
		}
	})
}
