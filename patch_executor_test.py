import os

files = ['internal/agent/executor/executor_test.go']

for file in files:
    with open(file, 'r') as f:
        content = f.read()

    old_code = "pipeline.(*defaultToolPipeline)"
    new_code = "pipeline.(*CircuitBreakerPipeline).next.(*defaultToolPipeline)"
    
    content = content.replace(old_code, new_code)
    
    with open(file, 'w') as f:
        f.write(content)
