// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// defaultPortsRegistryPath is the location of the ports registry comment
// block relative to the module root (read by loadPortsRegistry).
var defaultPortsRegistryPath = filepath.Join("internal", "domain", "ports", "doc.go")

// loadPortsRegistry reads and parses the `// # Registry` block of
// internal/domain/ports/doc.go, anchored at the module root via
// findModuleRoot. A missing or unparseable registry is an error (the gate
// cannot run without the architect's curated roster).
func loadPortsRegistry() (*portsRegistry, error) {
	root, err := findModuleRoot()
	if err != nil {
		return nil, fmt.Errorf("ports registry: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(root, defaultPortsRegistryPath))
	if err != nil {
		return nil, fmt.Errorf("ports registry: %w", err)
	}
	return parsePortsRegistry(string(data))
}

// portsRegistryFamily is one `## Family:` bucket in the ports registry.
type portsRegistryFamily struct {
	Name    string
	Members []string
}

// portsRegistry is the parsed membership roster of internal/domain/ports
// (issue #1343, ADR-064). Every exported symbol must appear in exactly one
// bucket: an interface type in exactly one family, or a non-interface export
// in Supporting. verify-ports-registry enforces the bijection against the
// live indexer.
type portsRegistry struct {
	Families   []portsRegistryFamily
	Supporting []string
}

// portsRegistryFamilyLimit is the structural bound (N ≤ 12) on distinct
// family names enforced by verifyPortsRegistryFamilyBound.
const portsRegistryFamilyLimit = 12

// parsePortsRegistry parses the `// # Registry` block of
// internal/domain/ports/doc.go. Prose before the start delimiter is ignored;
// the first non-blank line that matches none of the block line kinds
// terminates the block (that line and everything after is prose, ignored).
//
// Block line kinds:
//   - `// ## Family: <Name>` opens a family (name non-empty, families unique)
//   - `// ## Supporting` opens the non-interface bucket (at most once)
//   - `//   - <Name>` adds one member to the open family/supporting bucket
//     (name non-empty, names unique across the whole registry, and a bullet
//     must follow a family/supporting marker)
//   - `//` (blank separator, as inserted by gofmt) is skipped
//
// Malformed directives are hard errors, never silently skipped:
//   - `// ## Family:` with an empty name
//   - `// ## <anything>` that is not Family:/Supporting
//   - `//   - ` with an empty name
//   - a bullet before any family/supporting marker
func parsePortsRegistry(data string) (*portsRegistry, error) {
	reg := &portsRegistry{}
	familySeen := make(map[string]bool)
	nameSeen := make(map[string]bool)

	var (
		inBlock        bool
		inSupporting   bool
		seenSupporting bool
		curFamily      *portsRegistryFamily
	)

	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)

		if !inBlock {
			if trimmed == "// # Registry" {
				inBlock = true
			}
			continue
		}

		if trimmed == "" || trimmed == "//" {
			continue // blank separator (gofmt inserts these)
		}

		if strings.HasPrefix(trimmed, "// ##") {
			directive := strings.TrimSpace(strings.TrimPrefix(trimmed, "// ##"))
			if strings.HasPrefix(directive, "Family:") {
				name := strings.TrimSpace(strings.TrimPrefix(directive, "Family:"))
				if name == "" {
					return nil, fmt.Errorf("ports registry: empty family name")
				}
				if familySeen[name] {
					return nil, fmt.Errorf("ports registry: duplicate family %q", name)
				}
				familySeen[name] = true
				reg.Families = append(reg.Families, portsRegistryFamily{Name: name})
				curFamily = &reg.Families[len(reg.Families)-1]
				inSupporting = false
				continue
			}
			if directive == "Supporting" {
				if seenSupporting {
					return nil, fmt.Errorf("ports registry: duplicate Supporting marker")
				}
				seenSupporting = true
				inSupporting = true
				curFamily = nil
				continue
			}
			return nil, fmt.Errorf("ports registry: malformed directive %q", directive)
		}

		if strings.HasPrefix(trimmed, "//   - ") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "//   - "))
			if name == "" {
				return nil, fmt.Errorf("ports registry: empty name bullet")
			}
			if !inSupporting && curFamily == nil {
				return nil, fmt.Errorf("ports registry: name bullet %q before any family or Supporting marker", name)
			}
			if nameSeen[name] {
				return nil, fmt.Errorf("ports registry: duplicate symbol %q", name)
			}
			nameSeen[name] = true
			if inSupporting {
				reg.Supporting = append(reg.Supporting, name)
			} else {
				curFamily.Members = append(curFamily.Members, name)
			}
			continue
		}

		// First non-blank line matching none of the block kinds terminates
		// the block; it and everything after is prose.
		break
	}

	return reg, nil
}

// verifyPortsRegistryBijection checks the parsed registry against the live
// declarations and returns a slice of violation strings (empty = pass).
// decls is pre-filtered to ports-package exports whose symType is Type,
// Function, Constant, or Variable (Method and Unknown are out of scope in
// both directions). The four violation classes, each with a distinct message:
//
//   - phantom: a registry name that resolves to no declaration.
//   - omission: a live export present in no bucket.
//   - misclassification: a family member that is not an interface, or a
//     Supporting entry that is not a non-interface.
//   - duplicate: a live export listed in more than one bucket (cross-bucket;
//     the parser already rejects within-bucket and same-name duplicates).
func verifyPortsRegistryBijection(reg *portsRegistry, decls []*symMeta) []string {
	if reg == nil {
		reg = &portsRegistry{}
	}

	declByName := make(map[string]*symMeta, len(decls))
	for _, d := range decls {
		if d != nil {
			declByName[d.name] = d
		}
	}

	var violations []string
	bucketOf := make(map[string]string)

	for i := range reg.Families {
		fam := &reg.Families[i]
		bucket := fmt.Sprintf("family %q", fam.Name)
		for _, name := range fam.Members {
			if prev, dup := bucketOf[name]; dup {
				violations = append(violations, fmt.Sprintf("duplicate: live export %q listed in both %s and %s", name, prev, bucket))
				continue
			}
			bucketOf[name] = bucket
			d, ok := declByName[name]
			if !ok {
				violations = append(violations, fmt.Sprintf("phantom: registry entry %q has no live declaration", name))
				continue
			}
			if !d.isInterfaceType {
				violations = append(violations, fmt.Sprintf("misclassification: family %q member %q is not an interface", fam.Name, name))
			}
		}
	}

	for _, name := range reg.Supporting {
		if prev, dup := bucketOf[name]; dup {
			violations = append(violations, fmt.Sprintf("duplicate: live export %q listed in both %s and Supporting", name, prev))
			continue
		}
		bucketOf[name] = "Supporting"
		d, ok := declByName[name]
		if !ok {
			violations = append(violations, fmt.Sprintf("phantom: registry entry %q has no live declaration", name))
			continue
		}
		if d.isInterfaceType {
			violations = append(violations, fmt.Sprintf("misclassification: Supporting entry %q is an interface, not a non-interface export", name))
		}
	}

	for _, d := range decls {
		if d == nil {
			continue
		}
		if _, ok := bucketOf[d.name]; !ok {
			violations = append(violations, fmt.Sprintf("omission: live export %q is not present in the registry", d.name))
		}
	}

	sort.Strings(violations)
	return violations
}

// verifyPortsRegistryFamilyBound enforces the N ≤ 12 structural bound on
// distinct family names.
func verifyPortsRegistryFamilyBound(reg *portsRegistry) []string {
	if reg == nil || len(reg.Families) <= portsRegistryFamilyLimit {
		return nil
	}
	return []string{fmt.Sprintf("ports registry has %d families; limit is %d", len(reg.Families), portsRegistryFamilyLimit)}
}

// verifyPortsRegistryStayKeyLiveness enforces that every ADR-056 stay key in
// exitStayRationales (Capturer, HistoryBrowser, HistoryEditor, UIRenderer,
// SessionFinalizer, HistoryRenderer) names a live ports interface declaration
// in decls. This closes the silent 6→5 deletion gap: a removed stay key must
// surface as an explicit violation naming the missing key.
func verifyPortsRegistryStayKeyLiveness(decls []*symMeta) []string {
	interfaceNames := make(map[string]bool)
	for _, d := range decls {
		if d != nil && d.isInterfaceType {
			interfaceNames[d.name] = true
		}
	}

	keys := make([]string, 0, len(exitStayRationales))
	for k := range exitStayRationales {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var violations []string
	for _, k := range keys {
		if !interfaceNames[k] {
			violations = append(violations, fmt.Sprintf("stay key %q has no live ports interface declaration", k))
		}
	}
	return violations
}

// isPortsDeclaredType reports whether t (after unwrapping pointers and
// aliases) is a named type declared in internal/domain/ports.
func isPortsDeclaredType(t types.Type) bool {
	named, ok := unwrapPortsType(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return isPortsPackagePath(obj.Pkg().Path())
}

// unwrapPortsType unwraps pointers and aliases until it reaches a
// non-pointer, non-alias type (or nil).
func unwrapPortsType(t types.Type) types.Type {
	for t != nil {
		u := types.Unalias(t)
		if ptr, ok := u.(*types.Pointer); ok {
			t = ptr.Elem()
			continue
		}
		return u
	}
	return nil
}

// referencesPortsType reports whether t references any ports-declared type:
// dereference pointers/aliases; a ports named type → true; recurse into
// struct fields, signature params/results, map keys/values, and slice/array
// elems, but TERMINATE at a non-ports named type (do not recurse into its
// internals — ports-only scope). Used by clauses (a) and (c).
func referencesPortsType(t types.Type) bool {
	found := false
	visitPortsTypeReferences(t, func(string) {
		found = true
	})
	return found
}

// visitPortsTypeReferences walks t and invokes fn for the name of every
// ports-declared named type it references (ports-only scope). It dereferences
// pointers/aliases, reports ports named types, recurses into struct fields,
// signature params/results, interface methods/embeddeds, map keys/values, and
// slice/array elems, and TERMINATES at non-ports named types.
func visitPortsTypeReferences(t types.Type, fn func(name string)) {
	t = unwrapPortsType(t)
	if t == nil {
		return
	}
	switch tt := t.(type) {
	case *types.Named:
		if isPortsDeclaredType(tt) {
			fn(tt.Obj().Name())
			return
		}
		return // terminate at a non-ports named type
	case *types.Struct:
		for i := 0; i < tt.NumFields(); i++ {
			visitPortsTypeReferences(tt.Field(i).Type(), fn)
		}
	case *types.Signature:
		if params := tt.Params(); params != nil {
			for i := 0; i < params.Len(); i++ {
				visitPortsTypeReferences(params.At(i).Type(), fn)
			}
		}
		if results := tt.Results(); results != nil {
			for i := 0; i < results.Len(); i++ {
				visitPortsTypeReferences(results.At(i).Type(), fn)
			}
		}
	case *types.Interface:
		for i := 0; i < tt.NumMethods(); i++ {
			visitPortsTypeReferences(tt.Method(i).Type(), fn)
		}
		for i := 0; i < tt.NumEmbeddeds(); i++ {
			visitPortsTypeReferences(tt.EmbeddedType(i), fn)
		}
	case *types.Map:
		visitPortsTypeReferences(tt.Key(), fn)
		visitPortsTypeReferences(tt.Elem(), fn)
	case *types.Slice:
		visitPortsTypeReferences(tt.Elem(), fn)
	case *types.Array:
		visitPortsTypeReferences(tt.Elem(), fn)
	}
}

// collectReferencedPortsTypeNames returns the set of ports-declared named type
// names referenced within t (ports-only scope).
func collectReferencedPortsTypeNames(t types.Type) map[string]bool {
	names := make(map[string]bool)
	visitPortsTypeReferences(t, func(name string) {
		names[name] = true
	})
	return names
}

// supportingShape returns the walkable shape of a Supporting type: the
// underlying struct or signature (or other non-named underlying), so the
// walker recurses into fields/params/results rather than reporting the named
// type itself.
func supportingShape(t types.Type) types.Type {
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		return named.Underlying()
	}
	return t
}

// evaluateSupportingClauses evaluates the pure admission clauses (b, c, d, a)
// for every non-interface export in nonInterfaces and returns a map from
// symbol name to the admitting clause ("b", "c", "d", or "a"). Symbols not
// admitted by a pure clause are absent; clause "e" requires the live indexer
// and is handled by verifyPortsSupportingAdmission.
func evaluateSupportingClauses(interfaces map[string]*types.Interface, nonInterfaces []*symMeta) map[string]string {
	admitted := make(map[string]string)

	typeEntryByName := make(map[string]*symMeta)
	for _, m := range nonInterfaces {
		if m != nil && m.symType == "Type" {
			typeEntryByName[m.name] = m
		}
	}

	// Clauses (b), (c), (d) — per-entry, in order.
	for _, m := range nonInterfaces {
		if m == nil {
			continue
		}
		if clause := pureAdmissionClause(m, interfaces); clause != "" {
			admitted[m.name] = clause
		}
	}

	clauseAFixpoint(interfaces, typeEntryByName, admitted)
	return admitted
}

// pureAdmissionClause evaluates clauses (b), (c), and (d) for a single
// non-interface symMeta and returns the admitting clause label ("" if none).
func pureAdmissionClause(m *symMeta, interfaces map[string]*types.Interface) string {
	switch m.symType {
	case "Constant", "Variable":
		return "b"
	case "Function":
		fn, ok := m.obj.(*types.Func)
		if ok && referencesPortsType(fn.Type()) {
			return "c"
		}
		return ""
	case "Type":
		tn, ok := m.obj.(*types.TypeName)
		if !ok {
			return ""
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			return ""
		}
		if _, isIface := named.Underlying().(*types.Interface); isIface {
			return "" // interfaces are not non-interface exports
		}
		// (c) func-type declaration whose signature references a ports type.
		if sig, ok := named.Underlying().(*types.Signature); ok && referencesPortsType(sig) {
			return "c"
		}
		// (d) named type whose method set implements a ports interface.
		for _, iface := range interfaces {
			if iface == nil {
				continue
			}
			if types.Implements(named, iface) || types.Implements(types.NewPointer(named), iface) {
				return "d"
			}
		}
		return ""
	default:
		return ""
	}
}

// clauseAFixpoint computes the clause (a) admission fixpoint. The seed shapes
// are the ports interfaces plus every non-interface Type entry already
// admitted by clauses (b)/(c)/(d). Walking each admitted shape's struct
// fields/signature admits any referenced ports-declared non-interface type,
// iterating until no new admissions occur (clause-interleaved seed set).
func clauseAFixpoint(interfaces map[string]*types.Interface, typeEntryByName map[string]*symMeta, admitted map[string]string) {
	shapes := make([]types.Type, 0, len(interfaces)+len(typeEntryByName))
	for _, iface := range interfaces {
		if iface != nil {
			shapes = append(shapes, iface)
		}
	}
	for name, m := range typeEntryByName {
		if _, ok := admitted[name]; !ok {
			continue
		}
		if tn, ok := m.obj.(*types.TypeName); ok {
			shapes = append(shapes, supportingShape(tn.Type()))
		}
	}

	for {
		added := make([]string, 0)
		for _, shape := range shapes {
			for name := range collectReferencedPortsTypeNames(shape) {
				if _, ok := typeEntryByName[name]; !ok {
					continue
				}
				if _, already := admitted[name]; already {
					continue
				}
				admitted[name] = "a"
				added = append(added, name)
			}
		}
		if len(added) == 0 {
			return
		}
		for _, name := range added {
			if tn, ok := typeEntryByName[name].obj.(*types.TypeName); ok {
				shapes = append(shapes, supportingShape(tn.Type()))
			}
		}
	}
}

// verifyPortsSupportingAdmission evaluates the 5-clause admission rule for
// every non-interface export and returns violation strings (empty = all
// admitted). reg provides the Supporting list; decls provides all ports
// exports (interfaces and non-interfaces) with their types.Object.
func (a *defaultDeadCodeAnalyzer) verifyPortsSupportingAdmission(
	reg *portsRegistry,
	decls []*symMeta,
	state *scanState,
	hb chan<- struct{},
) []string {
	if reg == nil {
		reg = &portsRegistry{}
	}
	if state == nil {
		state = &scanState{}
	}

	interfaces := make(map[string]*types.Interface)
	nonInterfacesByName := make(map[string]*symMeta)
	var nonInterfaces []*symMeta
	for _, d := range decls {
		if d == nil {
			continue
		}
		if d.isInterfaceType {
			if tn, ok := d.obj.(*types.TypeName); ok {
				if iface, ok := tn.Type().Underlying().(*types.Interface); ok {
					interfaces[d.name] = iface
				}
			}
			continue
		}
		nonInterfaces = append(nonInterfaces, d)
		nonInterfacesByName[d.name] = d
	}

	admitted := evaluateSupportingClauses(interfaces, nonInterfaces)

	// Clause (e): cross-layer reference via usage sites (di-included).
	fileToPkg := a.buildFileToPkgMap(state.pkgs)
	for i, m := range nonInterfaces {
		if _, ok := admitted[m.name]; ok {
			continue
		}
		sendHeartbeat(i, hb)
		if a.supportingCrossLayerReference(m, state, fileToPkg, hb) {
			admitted[m.name] = "e"
		}
	}

	var violations []string
	for _, name := range reg.Supporting {
		if _, ok := nonInterfacesByName[name]; !ok {
			continue // phantom or misclassified entry — the bijection's job
		}
		if _, ok := admitted[name]; !ok {
			violations = append(violations, fmt.Sprintf("supporting entry %q not admitted by any clause", name))
		}
	}
	sort.Strings(violations)
	return violations
}

// supportingCrossLayerReference evaluates clause (e): the export's usage
// sites span more than one distinct layer. Composition roots are intentionally
// NOT excluded (di-included), per ADR-064.
func (a *defaultDeadCodeAnalyzer) supportingCrossLayerReference(meta *symMeta, state *scanState, fileToPkg map[string]string, hb chan<- struct{}) bool {
	usages, err := a.idx.GetUsages(context.Background(), meta.id, state.targetPath, hb)
	if err != nil {
		return false
	}
	layers := make(map[string]bool)
	for _, loc := range usages {
		pkg, ok := fileToPkg[loc.Path]
		if !ok {
			continue
		}
		base := getBasePkgPath(pkg)
		layers[classifyExitLayer(base, state.targetModule)] = true
	}
	return len(layers) > 1
}
