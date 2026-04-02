import re

with open('internal/agent/executor/pipeline_builder.go', 'r') as f:
    content = f.read()

# Remove newFailureTracker
content = re.sub(r'failures := newFailureTracker\(3\)\n', '', content)

# Remove failures: failures,
content = re.sub(r'failures:\s*failures,\n', '', content)

# Wrap pipeline with CircuitBreakerPipeline
pipeline_init_pattern = """	pipeline := &defaultToolPipeline{
		resolver:   newToolResolutionService(registry),
		authorizer: authService,
		runtime:    exec,
		registry:   registry,
	}"""
new_pipeline_init = """	basePipeline := &defaultToolPipeline{
		resolver:   newToolResolutionService(registry),
		authorizer: authService,
		runtime:    exec,
		registry:   registry,
	}
	pipeline := NewCircuitBreakerPipeline(basePipeline, 3, 5*time.Minute)"""

content = content.replace(pipeline_init_pattern, new_pipeline_init)

with open('internal/agent/executor/pipeline_builder.go', 'w') as f:
    f.write(content)
