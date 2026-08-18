// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"regexp"
	"strings"
)

// redactedValue is the placeholder logged in place of a secret-bearing value.
const redactedValue = "[REDACTED]"

// secretKeyPattern is the deny-list: suffix-anchored on the leaf,
// case-insensitive. `tokens?` is deliberately absent so max_tokens /
// max_history_tokens (non-secret integer limits) never match; token$ singular
// stays. NOTE: the trailing keys? alternative also matches any leaf ending in
// "key"/"keys" (e.g. "monkey") — accepted fail-closed over-redaction. The
// args?/env alternatives cover MCP stdio ARGS/ENV config, over-redacting any
// leaf ending in arg/args/env (e.g. an ARGS list that holds no secrets) —
// same accepted fail-closed trade as keys?.
var secretKeyPattern = regexp.MustCompile(`(?i)(api[_-]?keys?|auth[_-]?tokens?|authorization|token|passwords?|secrets?|credentials?|usernames?|keys?|args?|env)$`)

// isSecretKey reports whether a config key is secret-bearing.
// The regex is suffix-anchored and case-insensitive, so full viper paths
// (e.g. "mcp_servers::atlassian::token", "providers::google::apikey")
// match on their leaf without pre-splitting.
func isSecretKey(key string) bool {
	return secretKeyPattern.MatchString(strings.TrimSpace(key))
}

// hasSecretAncestor reports whether any proper ancestor of key (split on the
// viper "::" key delimiter) is deny-listed. Example: the parsed dump must drop
// mcp_servers::fs::env::FOO because its ancestor mcp_servers::fs::env has the
// deny-listed leaf "env" — a leaf-suffix match on FOO alone cannot see it.
func hasSecretAncestor(key string) bool {
	parts := strings.Split(key, "::")
	for i := 1; i < len(parts); i++ {
		if isSecretKey(strings.Join(parts[:i], "::")) {
			return true
		}
	}
	return false
}

// normalizeKeyToken normalizes a raw key token for deny-list comparison:
// trim, strip a "- " sequence prefix, take the part before the first ':' ,
// trim again, and strip surrounding single/double quote characters. Used by
// the raw-content parser for key extraction.
func normalizeKeyToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "- ")
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	return strings.Trim(s, `"'`)
}

// redactFlowEntry scans an unquoted value for a deny-listed name followed by
// ':' (a sub-key pattern, e.g. "Authorization:" inside HEADERS) and returns
// the redacted extent plus the preserved suffix. redactFlowEntry redacts from
// the matched sub-entry through its depth-0 extent (to the next depth-0
// comma/brace/EOL), preserving siblings.
func redactFlowEntry(rest string) string {
	start := firstSecretTokenIndex(rest)
	if start < 0 {
		return rest
	}
	if strings.Contains(rest[:start], "{") {
		end := flowExtentEnd(rest, start)
		return rest[:start] + redactedValue + rest[end:]
	}
	// Brace-less chain (or a plain scalar containing a deny-listed name):
	// redact the whole value, preserving only leading whitespace.
	return rest[:leadingWhitespaceLen(rest)] + redactedValue
}

// firstSecretTokenIndex returns the start index of the first deny-listed
// name token in rest, or -1 when no token is deny-listed.
func firstSecretTokenIndex(rest string) int {
	for i := 0; i < len(rest); {
		for i < len(rest) && !isTokenChar(rest[i]) {
			i++
		}
		start := i
		for i < len(rest) && isTokenChar(rest[i]) {
			i++
		}
		if start < i && isSecretKey(rest[start:i]) {
			return start
		}
	}
	return -1
}

// isTokenChar reports whether c can appear inside a config name token.
// Underscore is a token char so api_key stays one token; punctuation
// (':', '=', '?', '/', '-', braces, commas) splits tokens.
func isTokenChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// flowExtentEnd returns the index just past the depth-0 extent that begins
// at start: the next depth-0 comma, the closing brace of the enclosing flow,
// or the end of the string.
func flowExtentEnd(rest string, start int) int {
	depth := 0
	for i := start; i < len(rest); i++ {
		if rest[i] == '{' {
			depth++
		} else if rest[i] == '}' {
			if depth == 0 {
				return i
			}
			depth--
		} else if rest[i] == ',' && depth == 0 {
			return i
		}
	}
	return len(rest)
}

// leadingWhitespaceLen returns the number of leading space/tab characters.
func leadingWhitespaceLen(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// redactRawContent redacts secret-bearing values from a raw config dump.
// Line-oriented parser contract (each rule pinned by redact_test.go):
//
//  1. Split on '\n'; process each element; rejoin with '\n'. Blank or
//     whitespace-only elements (incl. the final element from a trailing
//     newline) pass through byte-identically — NEVER index
//     strings.Fields(body)[0] without a len(fields) == 0 guard.
//  2. Key extraction per line: trim; strip a leading "- " sequence prefix
//     (preserve the prefix in output); take the part before the first ':';
//     normalizeKeyToken. If no ':' exists, fall to rule 5 (colonless scan).
//  3. Key-mode redaction: if the extracted key is deny-listed (isSecretKey),
//     emit "<preserved-indent><preserved-prefix><normalized-key>: [REDACTED]"
//     and enter suppression: subsequent lines that are MORE indented than
//     this line's key column are dropped; subsequent lines at EQUAL indent
//     that are NOT key-shaped (no ':') are dropped (invalid-YAML
//     de-indented block scalars); a key-shaped equal-indent line terminates
//     suppression and is processed normally. A less-indented line also
//     terminates suppression.
//  4. Value-mode scan (key NOT deny-listed): let rest = text after the first
//     ':'. If strings.TrimSpace(rest) starts with a double or single quote
//     (quoted-scalar gate) OR is empty, the line passes through
//     byte-identically. Otherwise scan rest for a deny-listed name+':' sub-key
//     (redactFlowEntry); if one is found, replace from the sub-entry through
//     its depth-0 extent with redactedValue, preserving any prefix (e.g. '{')
//     and siblings after the extent. Brace-less chains
//     ("MCP_SERVERS: atlassian: TOKEN: file-token") redact to end of line.
//  5. Colonless scan (invalid-YAML class): fields := strings.Fields(line);
//     if len(fields) == 0 pass through; else if isSecretKey(fields[0]) emit
//     "<fields[0]> [REDACTED]"; else pass through byte-identically.
//
// Accepted residual class (documented, pinned by boundary tests):
//
//	(i)   innocuous-name scalars (PAYLOAD: sk-1234) pass through;
//	(ii)  key-shaped malformed-block content inside a block scalar is treated
//	      as a key line (fail-closed over-redaction of prose);
//	(iii) barred "tokens:" flow sub-keys are NOT redacted (kept off the
//	      deny-list to protect max_tokens);
//	plus unquoted plain-scalar URLs containing "token:" are over-redacted
//	(fail-closed; compensated in valid YAML by the parsed dump, whose "url"
//	leaf is not deny-listed).
func redactRawContent(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	suppressing := false
	keyColumn := 0
	for _, line := range lines {
		if isBlankLine(line) {
			out = append(out, line)
			continue
		}
		indent := leadingWhitespaceLen(line)
		rest := line[indent:]
		prefix, rest := stripSequencePrefix(rest)
		if suppressing && shouldSuppressDrop(indent, keyColumn, rest) {
			continue
		}
		suppressing = false
		colonIdx := strings.Index(rest, ":")
		if colonIdx < 0 {
			if redacted, ok := redactColonless(line); ok {
				out = append(out, redacted)
			} else {
				out = append(out, line)
			}
			continue
		}
		key := normalizeKeyToken(rest[:colonIdx])
		if isSecretKey(key) {
			out = append(out, line[:indent]+prefix+key+": "+redactedValue)
			suppressing = true
			keyColumn = indent
			continue
		}
		value := rest[colonIdx+1:]
		if isQuotedOrEmptyValue(value) {
			out = append(out, line)
			continue
		}
		if redacted := redactFlowEntry(value); redacted != value {
			out = append(out, line[:indent]+prefix+rest[:colonIdx+1]+redacted)
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// isBlankLine reports whether a line is empty or whitespace-only; such
// elements always pass through byte-identically (rule 1).
func isBlankLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

// stripSequencePrefix removes a leading "- " sequence prefix, returning it
// separately so callers can preserve it in the redacted output.
func stripSequencePrefix(s string) (string, string) {
	if strings.HasPrefix(s, "- ") {
		return "- ", s[2:]
	}
	return "", s
}

// shouldSuppressDrop reports whether a line is dropped while suppression is
// active: strictly more indented than the secret key's column, or at equal
// indent but not key-shaped (no ':').
func shouldSuppressDrop(indent, keyColumn int, rest string) bool {
	if indent > keyColumn {
		return true
	}
	return indent == keyColumn && !strings.Contains(rest, ":")
}

// redactColonless applies rule 5 to a colonless line, returning the redacted
// form and true when the first field is deny-listed.
func redactColonless(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 || !isSecretKey(fields[0]) {
		return "", false
	}
	return fields[0] + " " + redactedValue, true
}

// isQuotedOrEmptyValue reports whether the value text after a key's colon is
// empty or a quoted scalar; both pass through byte-identically (rule 4).
func isQuotedOrEmptyValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}
	return trimmed[0] == '"' || trimmed[0] == '\''
}
