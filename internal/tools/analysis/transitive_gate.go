// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// ADR-056, Decision 2: the architecture gate measures direct imports only;
// the transitive closure of internal/domain/ports is unenforced. Gate v1 is
// a consumer-level whitelist (docs/architect/TRANSITIVE_IMPORT_WHITELIST.md)
// that separates "decision required" rows (pending split-vs-accept) from
// "approved constant" rows (whitelisted or self-justifying). The report is
// authoritative; the issue's 26-payer / ~196-constant numbers are hypotheses
// and are never hardcoded here.
//
// Closure semantics: Dom(C) is the set of internal/domain/<family> packages
// reachable from consumer C over the module-internal import graph, with
// traversal THROUGH internal/domain/ports excluded — the hub is a whitelisted
// node, so its own closure is the derived constant and is NOT attributed to
// its importers. A consumer is self-justifying when its closure adds no
// family its direct domain imports do not already justify.
//
// The gate's evaluation API (buildInternalImportGraph, domainClosure, classifyConsumer, ...) is package-private: its only consumers are the in-package arch tests (real_architecture_test.go, transitive_gate_test.go). Do not re-export without an architect decision.

// portsConsumerPath is the module-relative path of the derived-constant hub.
const portsConsumerPath = "internal/domain/ports"

// derivedAllowedMarker is the literal value of the ports entry's allowed
// list: the gate computes Dom(ports) instead of reading a fixed list.
const derivedAllowedMarker = "<derived>"

// derivedPortsFamilies is the documented 9-family transitive closure of
// internal/domain/ports (ADR-056, Decision 2): the hub's closure must equal
// this set. The gate treats it as a fixed point — if ports' closure ever
// grows beyond these 9 families, the gate reports it as accidental growth
// that becomes decided policy only when the architect ratifies it.
//
// Read-only: treat as a constant.
var derivedPortsFamilies = []string{
	"config", "events", "llm", "persistence", "pricing",
	"security", "skills", "telemetry", "tools",
}

// defaultTransitiveWhitelistPath is the location of the architect-curated
// transitive-import whitelist relative to the module root.
var defaultTransitiveWhitelistPath = filepath.Join("docs", "architect", "TRANSITIVE_IMPORT_WHITELIST.md") //nolint:unused // consumed only via loadTransitiveWhitelist by the arch-tagged gate test

// consumerStatus is the classification outcome for one consumer package.
type consumerStatus string

const (
	// statusApprovedConstant marks a consumer whose closure is fully
	// justified: whitelisted (dom ⊆ allowed), self-justifying (dom ⊆
	// direct domain imports), or the derived-constant hub itself.
	statusApprovedConstant consumerStatus = "approved-constant"
	// statusDecisionRequired marks a consumer whose closure exceeds its
	// whitelist or its direct domain imports — a payer pending the
	// architect's split-vs-accept decision.
	statusDecisionRequired consumerStatus = "decision-required"
)

// whitelistEntry is one `## consumer:` block from the whitelist. Allowed is
// the sorted family list; Derived is true only for internal/domain/ports
// whose allowed value is the <derived> marker.
type whitelistEntry struct {
	Consumer string
	Allowed  []string
	Derived  bool
}

// transitiveWhitelist is the parsed architect-curated whitelist.
type transitiveWhitelist struct {
	// Decisions records the `## decision:` notes (e.g. "events → telemetry").
	Decisions []string
	// Consumers maps module-relative consumer paths to their entries.
	Consumers map[string]whitelistEntry
}

// entry returns the whitelist entry for consumer. Nil-safe.
func (w *transitiveWhitelist) entry(consumer string) (whitelistEntry, bool) {
	if w == nil {
		return whitelistEntry{}, false
	}
	e, ok := w.Consumers[consumer]
	return e, ok
}

// consumerClassification is one consumer's gate verdict.
type consumerClassification struct {
	Consumer    string
	Whitelisted bool
	Derived     bool // internal/domain/ports derived-constant row
	Dom         []string
	DirectDom   []string
	Allowed     []string
	Excess      []string
	Status      consumerStatus
	Detail      string
}

// consumerPath dedupes a package path to its base consumer path: the
// external-test "_test" suffix and the synthesized ".test" test-binary suffix
// are stripped so each package is one consumer. With go/packages Tests:true,
// a package directory yields up to four entries (base, in-package test
// variant with a bracketed ID, external `_test` package, and `.test` binary)
// whose edges all belong to the same consumer; the graph builder merges them.
func consumerPath(pkgPath string) string {
	pkgPath = strings.TrimSuffix(pkgPath, "_test")
	pkgPath = strings.TrimSuffix(pkgPath, ".test")
	return pkgPath
}

// detectModulePath returns the module path of the first package that
// declares one, mirroring indexedPackageProvider.detectModuleFromPackages.
func detectModulePath(pkgs []*packages.Package) string {
	for _, pkg := range pkgs {
		if pkg.Module != nil && pkg.Module.Path != "" {
			return pkg.Module.Path
		}
	}
	return ""
}

// isTrackedPackagePath mirrors architectureManager.isTrackedPackage: only
// module-internal paths containing "internal/" or "cmd/" participate in the
// gate graph.
func isTrackedPackagePath(pkgPath, modulePath string) bool {
	if !strings.HasPrefix(pkgPath, modulePath) {
		return false
	}
	return strings.Contains(pkgPath, "internal/") || strings.Contains(pkgPath, "cmd/")
}

// mergeUnique appends src to dst, deduplicating and preserving order.
func mergeUnique(dst, src []string) []string {
	for _, v := range src {
		found := false
		for _, d := range dst {
			if d == v {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
}

// buildInternalImportGraph builds the module-internal direct-import graph
// from the indexer's loaded packages, mirroring
// indexedPackageProvider.collectTrackedImports: for each tracked package the
// edges are its module-internal imports (module path prefix only; the
// internal//cmd/ filter is applied to the source side, as in the arch gate).
// Test variants (_test, .test, bracketed in-package variants) are deduplicated
// to their base consumer path and their edges merged, so each package is one
// consumer whose closure covers the production surface and its tests. The
// result is a map from full package path to a sorted, deduplicated list of
// module-internal import paths.
func buildInternalImportGraph(pkgs []*packages.Package, modulePath string) map[string][]string {
	graph := make(map[string][]string)
	for _, pkg := range pkgs {
		base := consumerPath(pkg.PkgPath)
		if !isTrackedPackagePath(base, modulePath) {
			continue
		}
		var imps []string
		for imp := range pkg.Imports {
			// Normalize the edge target to its consumer path so edges point
			// at canonical nodes (an import of pkg_test or pkg.test is an
			// edge to pkg itself).
			imp = consumerPath(imp)
			if strings.HasPrefix(imp, modulePath) && imp != base {
				imps = append(imps, imp)
			}
		}
		sort.Strings(imps)
		graph[base] = mergeUnique(graph[base], imps)
	}
	return graph
}

// familyOf returns the domain family of a module-relative path: the first
// path segment after "internal/domain/" (so events/eventstest → events), or
// "" when the path is not a domain-family package.
func familyOf(rel string) string {
	const prefix = "internal/domain/"
	if !strings.HasPrefix(rel, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(rel, prefix)
	if rest == "" {
		return ""
	}
	if i := strings.IndexByte(rest, '/'); i != -1 {
		rest = rest[:i]
	}
	return rest
}

// relPath trims the module path prefix into the module-relative form used by
// familyOf and the whitelist.
func relPath(pkgPath, modulePath string) string {
	rel := strings.TrimPrefix(pkgPath, modulePath)
	return strings.Trim(rel, "/")
}

// collectReachableNodes runs the closure BFS from start over graph, marking
// every visited node. When the ports hub is reached as an intermediate
// (cur != start && cur == ports) it is marked visited but NOT expanded: the
// hub is a whitelisted node whose own closure is the derived constant and is
// not attributed to its importers. When start IS ports (the derived-constant
// check), the hub's own edges ARE traversed so Dom(ports) is the derived
// constant.
func collectReachableNodes(graph map[string][]string, start, ports string) map[string]bool {
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur != start && cur == ports {
			// Hub reached as an intermediate: whitelisted node — do not
			// traverse through it, do not attribute its families.
			continue
		}
		for _, next := range graph[cur] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return visited
}

// extractDomainFamilies maps a visited node set to the sorted, deduplicated
// domain-family set of the closure: consumer and ports nodes are excluded
// (the ports family itself is never attributed), every other node maps via
// familyOf(relPath(...)), duplicates are dropped, and the result is sorted.
func extractDomainFamilies(nodes map[string]bool, consumer, ports, modulePath string) []string {
	var fams []string
	seenFam := make(map[string]bool)
	for node := range nodes {
		if node == consumer || node == ports {
			continue
		}
		if f := familyOf(relPath(node, modulePath)); f != "" && !seenFam[f] {
			seenFam[f] = true
			fams = append(fams, f)
		}
	}
	sort.Strings(fams)
	return fams
}

// domainClosure returns Dom(consumer): the sorted set of internal/domain/
// <family> packages reachable from consumer over the internal edge graph,
// with traversal THROUGH internal/domain/ports excluded. The ports hub is a
// whitelisted node: its own closure is the derived constant and is not
// attributed to its importers, and the ports family itself is never
// attributed either. When consumer IS ports (the derived-constant check), the
// hub's own edges are traversed so Dom(ports) is the derived constant.
func domainClosure(graph map[string][]string, consumer, modulePath string) []string {
	ports := modulePath + "/" + portsConsumerPath
	nodes := collectReachableNodes(graph, consumer, ports)
	return extractDomainFamilies(nodes, consumer, ports, modulePath)
}

// directDomainFamilies returns the sorted set of domain families of C's
// direct module-internal domain imports, with internal/domain/ports excluded
// (the hub's approval is the derived constant, not a direct import).
func directDomainFamilies(graph map[string][]string, consumer, modulePath string) []string {
	ports := modulePath + "/" + portsConsumerPath
	seen := make(map[string]bool)
	var fams []string
	for _, imp := range graph[consumer] {
		if imp == ports {
			continue
		}
		if f := familyOf(relPath(imp, modulePath)); f != "" && !seen[f] {
			seen[f] = true
			fams = append(fams, f)
		}
	}
	sort.Strings(fams)
	return fams
}

// setDiff returns the sorted elements of a that are not in b. Both inputs
// are expected sorted; correctness does not depend on it.
func setDiff(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, v := range b {
		inB[v] = true
	}
	var out []string
	for _, v := range a {
		if !inB[v] {
			out = append(out, v)
		}
	}
	return out
}

// familyNameRE validates whitelist family names (lowercase identifiers).
var familyNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// validConsumerPath reports whether a whitelist consumer path is a tracked
// module-relative path (internal/ or cmd/), mirroring isTrackedPackagePath.
func validConsumerPath(consumer string) bool {
	return strings.HasPrefix(consumer, "internal/") || strings.HasPrefix(consumer, "cmd/")
}

// parseFamilyList splits a comma-separated allowed list into a sorted,
// deduplicated family slice.
func parseFamilyList(list string) []string {
	seen := make(map[string]bool)
	var fams []string
	for _, part := range strings.Split(list, ",") {
		f := strings.TrimSpace(part)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		fams = append(fams, f)
	}
	sort.Strings(fams)
	return fams
}

// parseTransitiveWhitelist parses the architect-curated whitelist markdown.
// The pinned format:
//
//	# Transitive Import Whitelist — ADR-056, Decision 2
//	Architect-curated. ...
//	## decision: events → telemetry
//	First recorded decision: ...
//	## consumer: internal/ui/tui
//	allowed: llm
//	## consumer: internal/domain/ports
//	allowed: <derived>
//
// `## decision:` records a whitelist decision note; `## consumer:` opens a
// consumer block whose `allowed:` line lists its approved families (or the
// <derived> marker for internal/domain/ports). Headings, prose, and comments
// outside consumer blocks are ignored. Malformed input returns an error:
// allowed outside a consumer block, a consumer block without an allowed
// line, duplicate consumers, an empty or invalid allowed list, the derived
// marker on a non-ports consumer, an untracked consumer path, or prose
// inside a consumer block.
func parseTransitiveWhitelist(data string) (*transitiveWhitelist, error) {
	wl := &transitiveWhitelist{Consumers: make(map[string]whitelistEntry)}
	inConsumer := false
	curConsumer := ""

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var err error
		switch {
		case strings.HasPrefix(line, "## decision:"):
			inConsumer = false
			err = parseDecisionNote(line, wl)
		case strings.HasPrefix(line, "## consumer:"):
			var consumer string
			consumer, err = parseConsumerHeading(line, wl)
			curConsumer = consumer
			inConsumer = true
		case strings.HasPrefix(line, "allowed:"):
			err = applyAllowedDirective(line, wl, curConsumer, inConsumer)
		default:
			err = handleWhitelistBodyLine(line, wl, inConsumer)
		}
		if err != nil {
			return nil, err
		}
	}

	if err := validateWhitelistEntries(wl); err != nil {
		return nil, err
	}
	return wl, nil
}

// parseDecisionNote handles a `## decision:` directive: the note must be
// non-empty, is appended to Decisions, and the caller exits any open consumer
// block (state transition owned by the scanner loop).
func parseDecisionNote(line string, wl *transitiveWhitelist) error {
	note := strings.TrimSpace(strings.TrimPrefix(line, "## decision:"))
	if note == "" {
		return fmt.Errorf("transitive whitelist: empty decision note")
	}
	wl.Decisions = append(wl.Decisions, note)
	return nil
}

// parseConsumerHeading handles a `## consumer:` directive: the path must be a
// tracked internal/ or cmd/ path, the consumer must not already be registered
// (duplicate consumers are rejected), and the empty entry is registered
// before the consumer name is returned to the scanner loop.
func parseConsumerHeading(line string, wl *transitiveWhitelist) (string, error) {
	consumer := strings.TrimSpace(strings.TrimPrefix(line, "## consumer:"))
	if !validConsumerPath(consumer) {
		return "", fmt.Errorf("transitive whitelist: consumer %q is not a tracked internal/ or cmd/ path", consumer)
	}
	if _, dup := wl.Consumers[consumer]; dup {
		return "", fmt.Errorf("transitive whitelist: duplicate consumer %q", consumer)
	}
	wl.Consumers[consumer] = whitelistEntry{Consumer: consumer}
	return consumer, nil
}

// applyAllowedDirective handles an `allowed:` directive: it must appear inside
// an open consumer block and exactly once per consumer; the value is either
// the <derived> marker (valid only for internal/domain/ports) or a parsed
// family list.
func applyAllowedDirective(line string, wl *transitiveWhitelist, curConsumer string, inConsumer bool) error {
	if !inConsumer {
		return fmt.Errorf("transitive whitelist: allowed: line outside a consumer block")
	}
	entry := wl.Consumers[curConsumer]
	if entry.Derived || len(entry.Allowed) > 0 {
		return fmt.Errorf("transitive whitelist: duplicate allowed: for consumer %q", curConsumer)
	}
	list := strings.TrimSpace(strings.TrimPrefix(line, "allowed:"))
	if list == derivedAllowedMarker {
		return applyDerivedMarker(wl, curConsumer)
	}
	fams, err := parseAllowedFamilies(list, curConsumer)
	if err != nil {
		return err
	}
	entry.Allowed = fams
	wl.Consumers[curConsumer] = entry
	return nil
}

// applyDerivedMarker applies the <derived> marker to the ports entry: valid
// only for internal/domain/ports, whose closure the gate computes instead of
// reading a fixed list.
func applyDerivedMarker(wl *transitiveWhitelist, consumer string) error {
	if consumer != portsConsumerPath {
		return fmt.Errorf("transitive whitelist: <derived> is valid only for %s, got %q", portsConsumerPath, consumer)
	}
	entry := wl.Consumers[consumer]
	entry.Derived = true
	wl.Consumers[consumer] = entry
	return nil
}

// parseAllowedFamilies parses a non-derived allowed list into the consumer's
// sorted, deduplicated family slice: an empty list is an error, and every
// family name must match familyNameRE.
func parseAllowedFamilies(list, curConsumer string) ([]string, error) {
	fams := parseFamilyList(list)
	if len(fams) == 0 {
		return nil, fmt.Errorf("transitive whitelist: empty allowed: list for consumer %q", curConsumer)
	}
	for _, f := range fams {
		if !familyNameRE.MatchString(f) {
			return nil, fmt.Errorf("transitive whitelist: invalid family name %q in consumer %q", f, curConsumer)
		}
	}
	return fams, nil
}

// handleWhitelistBodyLine handles a non-directive body line: headings and
// comments are skipped; prose inside an open consumer block is an error;
// prose under the header or a decision note is ignored.
func handleWhitelistBodyLine(line string, wl *transitiveWhitelist, inConsumer bool) error {
	if strings.HasPrefix(line, "#") {
		return nil // headings and comments — not directives
	}
	if inConsumer {
		return fmt.Errorf("transitive whitelist: unexpected content in consumer block: %q", line)
	}
	// Prose under the header or a decision note — ignored.
	return nil
}

// validateWhitelistEntries enforces the trailing parse invariant: every
// non-derived consumer block must have an allowed: list.
func validateWhitelistEntries(wl *transitiveWhitelist) error {
	for consumer, e := range wl.Consumers {
		if !e.Derived && e.Allowed == nil {
			return fmt.Errorf("transitive whitelist: consumer %q has no allowed: list", consumer)
		}
	}
	return nil
}

// findModuleRoot walks up from the current working directory until it finds
// a go.mod file, returning the absolute path to the module root. Shared by
// the real-architecture tests and the transitive-closure gate loader.
func findModuleRoot() (string, error) { //nolint:unused // consumed only via loadTransitiveWhitelist by the arch-tagged gate test
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

// loadTransitiveWhitelist loads and parses
// docs/architect/TRANSITIVE_IMPORT_WHITELIST.md anchored at the module root
// via findModuleRoot — the loadNonFixCatalog precedent. Unlike the catalog,
// a missing or malformed whitelist is an error: the gate cannot run without
// the architect's curated decisions.
func loadTransitiveWhitelist() (*transitiveWhitelist, error) { //nolint:unused // sole caller real_architecture_test.go:66 is behind //go:build arch, invisible to non-arch lint
	root, err := findModuleRoot()
	if err != nil {
		return nil, fmt.Errorf("transitive whitelist: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(root, defaultTransitiveWhitelistPath))
	if err != nil {
		return nil, fmt.Errorf("transitive whitelist: %w", err)
	}
	return parseTransitiveWhitelist(string(data))
}

// classifyConsumer classifies one consumer given its closure dom, its direct
// domain families, and the whitelist:
//
//   - internal/domain/ports: derived-constant check — dom must equal the
//     documented 9 families; drift is accidental growth → decision required.
//   - whitelisted consumer: dom ⊆ allowed → approved constant; else decision
//     required with the excess families listed.
//   - non-whitelisted consumer: dom ⊆ directDom → approved constant
//     (self-justifying — the closure adds no family the consumer's direct
//     domain imports do not already justify); else decision required
//     (pure-bloat payer: the closure pulls families never directly imported).
func classifyConsumer(consumer string, dom, directDom []string, wl *transitiveWhitelist) consumerClassification {
	c := consumerClassification{
		Consumer:  consumer,
		Dom:       dom,
		DirectDom: directDom,
	}

	if consumer == portsConsumerPath {
		c.Derived = true
		excess := setDiff(dom, derivedPortsFamilies)
		missing := setDiff(derivedPortsFamilies, dom)
		c.Excess = excess
		if len(excess) == 0 && len(missing) == 0 {
			c.Status = statusApprovedConstant
			c.Detail = fmt.Sprintf("derived constant: closure equals documented %d families", len(derivedPortsFamilies))
			return c
		}
		c.Status = statusDecisionRequired
		c.Detail = fmt.Sprintf("derived-constant drift: closure %v ≠ documented %v (missing: %v)", dom, derivedPortsFamilies, missing)
		return c
	}

	if entry, ok := wl.entry(consumer); ok {
		c.Whitelisted = true
		c.Allowed = entry.Allowed
		excess := setDiff(dom, entry.Allowed)
		c.Excess = excess
		if len(excess) == 0 {
			c.Status = statusApprovedConstant
			c.Detail = "whitelisted: closure ⊆ allowed"
			return c
		}
		c.Status = statusDecisionRequired
		c.Detail = fmt.Sprintf("excess beyond whitelist: %v", excess)
		return c
	}

	excess := setDiff(dom, directDom)
	c.Excess = excess
	if len(excess) == 0 {
		c.Status = statusApprovedConstant
		c.Detail = "self-justifying: closure ⊆ direct domain imports"
		return c
	}
	c.Status = statusDecisionRequired
	c.Detail = fmt.Sprintf("pure-bloat payer: closure pulls families never directly imported: %v", excess)
	return c
}

// classifyAllConsumers classifies every consumer in the graph in sorted
// package order. The consumer path used for whitelist lookups and the ports
// derived check is the module-relative form.
func classifyAllConsumers(graph map[string][]string, wl *transitiveWhitelist, modulePath string) []consumerClassification {
	consumers := make([]string, 0, len(graph))
	for c := range graph {
		consumers = append(consumers, c)
	}
	sort.Strings(consumers)

	out := make([]consumerClassification, 0, len(consumers))
	for _, c := range consumers {
		rel := relPath(c, modulePath)
		dom := domainClosure(graph, c, modulePath)
		direct := directDomainFamilies(graph, c, modulePath)
		out = append(out, classifyConsumer(rel, dom, direct, wl))
	}
	return out
}

// joinList renders a family slice as a comma-separated list, or "—" when empty.
func joinList(fams []string) string {
	if len(fams) == 0 {
		return "—"
	}
	return strings.Join(fams, ", ")
}

// expectedColumn renders the "whitelist-or-expected" report column for a row.
func expectedColumn(c consumerClassification) string {
	switch {
	case c.Derived:
		return fmt.Sprintf("expected: derived-%d", len(derivedPortsFamilies))
	case c.Whitelisted:
		return "allowed: " + joinList(c.Allowed)
	default:
		return "expected: direct: " + joinList(c.DirectDom)
	}
}

// formatTransitiveGateReport renders the v1 gate report: a "decision
// required" section (consumer, whitelist-or-expected, closure, excess
// families — every payer row is pending split-vs-accept) and an "approved
// constant" section (count + list). Payer rows carry no invented rationales.
func formatTransitiveGateReport(classifications []consumerClassification, wl *transitiveWhitelist) string {
	var sb strings.Builder

	var decisions []string
	if wl != nil {
		decisions = wl.Decisions
	}
	_, _ = fmt.Fprintf(&sb, "=== Transitive Closure Gate — ADR-056 Decision 2 (v1, STRICT) ===\n\n")
	_, _ = fmt.Fprintf(&sb, "Whitelist decisions (%d):\n", len(decisions))
	if len(decisions) == 0 {
		sb.WriteString("  — none\n")
	} else {
		for _, d := range decisions {
			_, _ = fmt.Fprintf(&sb, "  - %s\n", d)
		}
	}

	var decisionRequired, approved []consumerClassification
	for _, c := range classifications {
		switch c.Status {
		case statusDecisionRequired:
			decisionRequired = append(decisionRequired, c)
		default:
			approved = append(approved, c)
		}
	}

	_, _ = fmt.Fprintf(&sb, "\nConsumers classified: %d | decision required: %d | approved constant: %d\n",
		len(classifications), len(decisionRequired), len(approved))

	_, _ = fmt.Fprintf(&sb, "\n— DECISION REQUIRED (%d) —\n", len(decisionRequired))
	sb.WriteString("| consumer | whitelist-or-expected | closure | excess families |\n")
	sb.WriteString("| --- | --- | --- | --- |\n")
	for _, c := range decisionRequired {
		_, _ = fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n",
			c.Consumer, expectedColumn(c), joinList(c.Dom), joinList(c.Excess))
	}

	_, _ = fmt.Fprintf(&sb, "\n— APPROVED CONSTANT (%d) —\n", len(approved))
	for _, c := range approved {
		_, _ = fmt.Fprintf(&sb, "  %s — %s\n", c.Consumer, c.Detail)
	}

	return sb.String()
}
