import re

with open('internal/agent/executor/zombie_test.go', 'r') as f:
    content = f.read()

old_code = "exec.pipeline.(*defaultToolPipeline)"
new_code = "exec.pipeline.(*CircuitBreakerPipeline).next.(*defaultToolPipeline)"

content = content.replace(old_code, new_code)

with open('internal/agent/executor/zombie_test.go', 'w') as f:
    f.write(content)
