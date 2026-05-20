# PR Review SOP

Automated AI review pipeline for pull requests. Triggered via tmg-kage `pr-reviewer-1`.

## Trigger

```
curl -X POST https://tmg-kage.ptc-nec.net \
  -H "x-api-key: eGAkSLDMbNpM6CBGYFk=" \
  -H "Content-Type: application/json" \
  -d '{"script":"pr-reviewer-1","config":"<PR-number>"}'
```

## Prerequisites

- `gh` CLI authenticated
- `tell-me-go` binary available
- `configs/reviewer.yaml` present
- Working directory: clean clone of tell-me-go on `dev`

## SOP Steps

### Phase 1: Fetch & Understand

1. `gh pr checkout <PR-number>`
2. `gh pr view <PR-number> --json title,body,additions,deletions,files,changedFiles`
3. Read the PR description and linked issue
4. `gh pr diff <PR-number>` — full diff

### Phase 2: Diagnostic Analysis

5. Run `go vet ./...` — capture all findings
6. Run `staticcheck ./...` — capture all findings
7. Run `go test ./...` — must pass
8. Run `go test -race ./...` — must pass
9. Check `golangci-lint run` if configured

### Phase 3: Code Review

10. **Security**: Check for injections, path traversal, exposed secrets, unsafe type assertions
11. **Idioms**: Early returns, error wrapping (`%w`), context propagation, nil handling
12. **Concurrency**: Goroutine leaks, race conditions, mutex usage, channel patterns
13. **Architecture**: Does the change respect package boundaries? Any new dependencies?

### Phase 4: Feedback

14. Categorize all findings:
    - `[CRITICAL]` — security issues, data loss, broken builds
    - `[HIGH]` — concurrency bugs, incorrect behavior, missing error handling
    - `[MEDIUM]` — style violations, missing tests, complexity concerns
15. For each finding, provide "Bad vs Good" code examples
16. If no findings above `[MEDIUM]`, the PR is ready for approval

### Phase 5: Post Review

17. Post review summary via `gh pr review <PR-number> --comment --body "..."`
18. If all clear: `gh pr review <PR-number> --approve --body "LGTM. ..."`
19. If issues found: post `--request-changes` with actionable fix instructions

## Design Decisions (Tracking: tmg-kage #1)

| Decision | Status |
|----------|--------|
| Read-only review or review+fix? | TBD |
| Auto-approve or human gate? | TBD |
| Provider/model for review? | TBD (default: Gemini Flash via reviewer.yaml) |
