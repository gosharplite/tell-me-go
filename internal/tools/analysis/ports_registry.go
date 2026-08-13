// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"sort"
	"strings"
)

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
