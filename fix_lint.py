import sys
import re

with open('/home/pos/tmp/bash-2-go/tell-me-go/internal/agent/executor/executor_test.go', 'r') as f:
    content = f.read()

old_if = """		if calls[i].Name == "success_tool" {
			assert.Equal(t, "success", tr.Text)
			foundSuccess = true
		} else if calls[i].Name == "panic_tool" {
			assert.Contains(t, tr.Text, "simulated nil pointer dereference")
			foundPanic = true
		}"""

new_switch = """		switch calls[i].Name {
		case "success_tool":
			assert.Equal(t, "success", tr.Text)
			foundSuccess = true
		case "panic_tool":
			assert.Contains(t, tr.Text, "simulated nil pointer dereference")
			foundPanic = true
		}"""

if old_if in content:
    content = content.replace(old_if, new_switch)
    with open('/home/pos/tmp/bash-2-go/tell-me-go/internal/agent/executor/executor_test.go', 'w') as f:
        f.write(content)
else:
    print("Not found")

