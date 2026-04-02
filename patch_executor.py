import re

with open('internal/agent/executor/executor.go', 'r') as f:
    content = f.read()

# Remove failureTracker from NewDefaultToolPipeline signature
content = re.sub(r'failures\s+\*failureTracker,\s*', '', content)

# Remove failureTracker from Orchestrator struct
content = re.sub(r'failures\s+\*failureTracker\n', '', content)

# Remove failureTracker definition and methods
tracker_pattern = re.compile(r'type failureTracker struct \{.*?\n\}\n\n.*?func \(f \*failureTracker\) Check\(toolName string\) error \{.*?\n\}\n', re.DOTALL)
content = re.sub(tracker_pattern, '', content)

# Replace the check logic in runExecutionPlan (serial)
serial_logic_old = """			if err := e.failures.Check(fc.Name); err != nil {
				resultsCh <- toolExecResult{index: taskIdx, name: fc.Name, tr: tools.ToolResult{Text: err.Error(), Error: err}}
			} else {
				tr := e.pipeline.ExecuteTool(ctx, fc)
				resultsCh <- toolExecResult{index: taskIdx, name: fc.Name, tr: tr}
			}"""
serial_logic_new = """			tr := e.pipeline.ExecuteTool(ctx, fc)
			resultsCh <- toolExecResult{index: taskIdx, name: fc.Name, tr: tr}"""
content = content.replace(serial_logic_old, serial_logic_new)

# Replace the check logic in runExecutionPlan (parallel)
parallel_logic_old = """				if err := e.failures.Check(fc.Name); err != nil {
					resultsCh <- toolExecResult{index: i, name: fc.Name, tr: tools.ToolResult{Text: err.Error(), Error: err}}
					continue
				}"""
content = content.replace(parallel_logic_old, "")

# Remove record from fan-in loop
record_logic = "\t\t\te.failures.Record(res.name, res.tr.Error == nil)\n"
content = content.replace(record_logic, "")

with open('internal/agent/executor/executor.go', 'w') as f:
    f.write(content)
