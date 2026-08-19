# ADR-069: MCP stdio ENV key case preservation (total Viper bypass)

- **Status**: Accepted
- **Date**: 2026-09
- **Deciders**: Architect, Coder
- **Consulted**: Issue #1407 (supersedes #1406; grill-round corrections)

## Context

- Viper v1.21.0 lowercases every config key recursively during `v.Unmarshal` —
  `insensitiviseMap` (`util.go:89-100`) and `getSettings`
  (`viper.go:1970-1987`) — so key casing authored in YAML is lost before the
  domain struct is populated.
- `MCP_SERVERS.<name>.ENV` is the only config map whose keys reach a
  **case-sensitive** consumer: `sortedEnvPairs`
  (`internal/infrastructure/mcp/stdio_client.go:354-365`) passes them
  verbatim into `cmd.Env` (`:77`), and Linux/macOS `execve` is
  case-sensitive. `LLMProvider.Headers` is RFC 9110 case-insensitive;
  `PROVIDERS`/`MODELS` keys are matched case-insensitively on both sides. No
  YAML casing can work around it.
- Live failure: the `plur` stdio server configured with
  `ENV: {PLUR_TOOL_PROFILE: "full"}` receives `plur_tool_profile=full`;
  `@plur-ai/mcp` serves the lean profile and `plur_inject_hybrid` fails.
- The bug was known and papered over when #1396 landed
  (`TestLoad_MCPStdio_EnvExpansion` asserted on the value rather than the key
  casing).

## Decision

### Total bypass

1. **Raw re-parse with the same YAML library.** After Viper's
   `unmarshalConfig`, `load()` re-parses the raw YAML with
   `gopkg.in/yaml.v3` (the same library Viper uses internally — no parser
   divergence) and `applyCasePreservingMCPServerEnv`
   (`internal/infrastructure/config/config.go`) stamps each decoded server's
   `Env` map **byte-for-byte** from the raw leaf keys. Viper never sees the
   ENV subtree as a decode authority.
2. **Deterministic collision rejection** at every navigation level
   (MCP_SERVERS root, server name, ENV container): sibling keys differing
   only by case (`Plur`/`plur`, `ENV`/`env`, `MCP_SERVERS`/`mcp_servers`)
   fail the load naming both raw keys — replacing Viper's previously
   *nondeterministic* random collapse winner. A lone case-normalized key
   (`Plur:` → `plur`) still loads.
3. **Env-override precedence (supported + documented):**
   `TELL_ME_MCP_SERVERS_<NAME>_ENV_<KEY>` wins for declared keys only; keys
   reach the child byte-for-byte regardless of source; overrides cannot
   invent undeclared variables; an override for case-differing keys applies
   to all casings; empty override values are ignored (AllowEmptyEnv parity).
   `os.ExpandEnv` applies on both the override and YAML branches, matching
   `expandEnvHook`.
4. **Wiring:** the bypass is invoked in `load()` after `unmarshalConfig` and
   before `ValidateMCPServers`; the watcher hot-reload re-enters `load()`, so
   it inherits the fix, and a bypass error keeps prior state per ADR-029 §5.
5. `stdio_client.go` is unchanged.

## Consequences

**Positive:** ENV keys reach stdio children byte-for-byte; case-sensitive
servers (plur) work as authored; the config surface is deterministic where it
was previously nondeterministic.

**Negative / accepted costs:**
(i) `load()` reads the config file twice (the second read feeds the bypass) —
accepted, startup/hot-reload only;
(ii) the bypass is a deliberate anti-Viper measure — a future viper minor
bump that re-breaks key casing is caught by the seven decode-path tests in
`config_test.go` and the `mcp-env-keys-byte-for-byte` domain-model invariant
(T5), failing CI loudly;
(iii) **structurally-unreachable defensive code** — `stringKeyMap`'s
`map[interface{}]interface{}` fallback branch (added in T1 for non-string
YAML keys like an unquoted `Null:`) cannot fire through `load()` today
because Viper's own YAML codec rejects non-string keys first; kept as a
defensive guard against Viper decode-behavior drift (INTENTIONAL_NON_FIXES
`structurally-unreachable` class), not test-pinned.

**Neutral:** case-colliding sibling keys that previously loaded
nondeterministically now fail loudly with a message naming both raw keys.

## References

- Issue #1407 (supersedes #1406) — MCP stdio ENV key case preservation
- ADR-029 — fallible `Reconfigure` delegate chain / hot-reload prior-state
  posture (`docs/adr/2026-05-fallible-reconfigure-delegate-chain.md`)
- ADR-068 — automatic PLUR memory integration, the first consumer of
  case-sensitive stdio ENV keys (`docs/adr/2026-09-automatic-plur-memory-integration.md`)
- Code seams: `internal/infrastructure/config/config.go` (`load`,
  `applyCasePreservingMCPServerEnv`, `stringKeyMap`, `unmarshalConfig`),
  `internal/infrastructure/mcp/stdio_client.go` (`cmd.Env`, `sortedEnvPairs`),
  `internal/infrastructure/config/config_test.go` (decode-path tests)
- Viper v1.21.0 (`go.mod`): `insensitiviseMap` (`util.go:89-100`),
  `getSettings` (`viper.go:1970-1987`)
- INTENTIONAL_NON_FIXES — `structurally-unreachable` acceptance class
  (`docs/architect/INTENTIONAL_NON_FIXES.md`)
