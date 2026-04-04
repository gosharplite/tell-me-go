## Step 1
Request Architect Review.
```bash
echo "Is the different between dev branch and main branch good enough to be merged into main branch?" | \
tell-me-go -new -r -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/architect.yaml &> /dev/null
```

## Step 2
Get responses from Architect
```bash
tell-me-go -l 3 -r -c /Users/johndoe/tmp/dualnets/seed/notebooks/mbp-johndoe/ait/yaml/architect.yaml
```

## Step 3
Did Step 1 command complete successfully?
Does Architect Review look ok?
