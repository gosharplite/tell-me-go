# Chatting with AI
This page describes how to chat with other AI roles in an agent loop of tell-me-go.

## Prerequisites
1. Role config file.
   **Example**: 
   ```bash
   export ARCHITECT_CONFIG=/path-to/architect.yaml
   export CODER_CONFIG=/path-to/coder.yaml
   ```
## Chatting with Roles
To send prompt to a role, use `tell‑me‑go`. The basic pattern is:

```bash
# Request action from Architect (output discarded)
tell-me-go -new -r -c ${ARCHITECT_CONFIG} < /tmp/prompt.txt &> /dev/null
```

**Notes:**
- **CRITICAL:** When using the `execute_command` tool to run `tell-me-go`, you MUST set the `timeout` parameter to `1800` (1800 seconds / 30 minutes) to ensure the sub-agent has sufficient time to complete its task. If the `timeout` parameter is not explicitly set, `execute_command` will default to a 15-second hard limit, which is typically not enough for complex AI sub-tasks and will result in premature cancellation.
- **Important:** Verify the prompt content is non‑empty and appropriate for the target role before forwarding it. Save prompts in a file then use input redirection (`<`) to tell-me-go. This can avoid parsing problems of using echo prompts directly to tell-me-go. Check /tmp/prompt.txt before sending it to tell-me-go.
- The `&> /dev/null` discards stdout and stderr to **keep token usage low** and keep the terminal clean while the role processes in the background. If you need to review what happened, run `tell-me-go -t -c ${ROLE_CONFIG}` (e.g. `${ARCHITECT_CONFIG}`) to output the session's execution log.
- Use `-new` when you first chat with a role. For a continuous conversation, omit `-new`.

To retrieve the last responses from a role:

```bash
# Retrieve the last 3 responses from the Architect
tell-me-go -l 3 -r -c ${ARCHITECT_CONFIG}
```

Adjust the number `-l 3` as needed.
