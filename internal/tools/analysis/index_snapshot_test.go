// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// jsonSymMeta is the JSON-serializable form of symMeta. All fields
// are exported and tagged for JSON marshaling. Converted to/from
// symMeta during snapshot save/load.
type jsonSymMeta struct {
	ID                  string `json:"id"`
	PkgPath             string `json:"pkg_path"`
	Name                string `json:"name"`
	SymType             string `json:"sym_type"`
	IsMethod            bool   `json:"is_method"`
	IsInterfaceType     bool   `json:"is_interface_type"`
	IsInterfaceMethod   bool   `json:"is_interface_method"`
	IsWellKnownContract bool   `json:"is_well_known_contract"`
}

func (m *symMeta) toJSON() *jsonSymMeta {
	return &jsonSymMeta{
		ID:                  m.id,
		PkgPath:             m.pkgPath,
		Name:                m.name,
		SymType:             m.symType,
		IsMethod:            m.isMethod,
		IsInterfaceType:     m.isInterfaceType,
		IsInterfaceMethod:   m.isInterfaceMethod,
		IsWellKnownContract: m.isWellKnownContract,
	}
}

func (j *jsonSymMeta) toSymMeta() *symMeta {
	return &symMeta{
		id:                  j.ID,
		pkgPath:             j.PkgPath,
		name:                j.Name,
		symType:             j.SymType,
		isMethod:            j.IsMethod,
		isInterfaceType:     j.IsInterfaceType,
		isInterfaceMethod:   j.IsInterfaceMethod,
		isWellKnownContract: j.IsWellKnownContract,
		obj:                 nil, // nil in fixture
	}
}

// indexSnapshotJSON is the pure JSON form of indexSnapshot.
// Used only for marshaling/unmarshaling to avoid complexity with
// unexported symMeta fields.
type indexSnapshotJSON struct {
	ModulePath    string                      `json:"module_path"`
	WorkspaceRoot string                      `json:"workspace_root"`
	Declarations  []*jsonSymMeta              `json:"declarations"`
	FileToPkg     map[string]string           `json:"file_to_pkg"`
	SymbolsByPath map[string][]symbolLocation `json:"symbols_by_path"`
	UsagesByName  map[string][]location       `json:"usages_by_name"`
	ImplsCache    map[string][]string         `json:"impls_cache"`
}

// indexSnapshot is a JSON-serializable capture of an indexer's complete
// internal state. It is used to persist and restore index data for test
// fixtures, bypassing the expensive packages.Load call.
type indexSnapshot struct {
	ModulePath    string
	WorkspaceRoot string
	Declarations  []*symMeta
	FileToPkg     map[string]string
	SymbolsByPath map[string][]symbolLocation
	UsagesByName  map[string][]location
	ImplsCache    map[string][]string
}

func (s *indexSnapshot) toJSON() *indexSnapshotJSON {
	j := &indexSnapshotJSON{
		ModulePath:    s.ModulePath,
		WorkspaceRoot: s.WorkspaceRoot,
		FileToPkg:     s.FileToPkg,
		SymbolsByPath: s.SymbolsByPath,
		UsagesByName:  s.UsagesByName,
		ImplsCache:    s.ImplsCache,
	}
	j.Declarations = make([]*jsonSymMeta, len(s.Declarations))
	for i, d := range s.Declarations {
		j.Declarations[i] = d.toJSON()
	}
	return j
}

func fromJSON(j *indexSnapshotJSON) *indexSnapshot {
	s := &indexSnapshot{
		ModulePath:    j.ModulePath,
		WorkspaceRoot: j.WorkspaceRoot,
		FileToPkg:     j.FileToPkg,
		SymbolsByPath: j.SymbolsByPath,
		UsagesByName:  j.UsagesByName,
		ImplsCache:    j.ImplsCache,
	}
	s.Declarations = make([]*symMeta, len(j.Declarations))
	for i, jd := range j.Declarations {
		s.Declarations[i] = jd.toSymMeta()
	}
	return s
}

// snapshot captures the indexer's current state as an indexSnapshot.
func (idx *indexer) snapshot() *indexSnapshot {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	fileToPkg := make(map[string]string)
	for _, pkg := range idx.pkgs {
		for _, f := range pkg.GoFiles {
			fileToPkg[f] = pkg.PkgPath
		}
		for _, f := range pkg.CompiledGoFiles {
			if _, ok := fileToPkg[f]; !ok {
				fileToPkg[f] = pkg.PkgPath
			}
		}
	}

	// Compute workspace root and rewrite file paths to be relative.
	wsRoot, err := filepath.Abs(idx.dir)
	if err != nil {
		wsRoot = idx.dir
	}
	relFileToPkg := make(map[string]string, len(fileToPkg))
	for filePath, pkgPath := range fileToPkg {
		if relPath, err := filepath.Rel(wsRoot, filePath); err == nil {
			relFileToPkg[relPath] = pkgPath
		}
	}

	modulePath := ""
	for _, pkg := range idx.pkgs {
		if pkg.Module != nil && pkg.Module.Path != "" {
			modulePath = pkg.Module.Path
			break
		}
	}

	var decls []*symMeta
	idx.collectDeclarations(func(meta *symMeta) bool {
		meta.obj = nil
		decls = append(decls, meta)
		return true
	})

	impls := idx.computeImplementationsLazy()
	implsCopy := make(map[string][]string, len(impls))
	for k, v := range impls {
		implsCopy[k] = v
	}

	symbolsCopy := make(map[string][]symbolLocation, len(idx.symbolsByPath))
	for k, v := range idx.symbolsByPath {
		symbolsCopy[k] = v
	}
	usagesCopy := make(map[string][]location, len(idx.usagesByName))
	for k, v := range idx.usagesByName {
		usagesCopy[k] = v
	}

	return &indexSnapshot{
		ModulePath:    modulePath,
		WorkspaceRoot: wsRoot,
		Declarations:  decls,
		FileToPkg:     relFileToPkg,
		SymbolsByPath: symbolsCopy,
		UsagesByName:  usagesCopy,
		ImplsCache:    implsCopy,
	}
}

// saveSnapshot writes the index snapshot to a JSON file.
func (s *indexSnapshot) saveSnapshot(path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(s.toJSON())
}

// loadSnapshot reads an index snapshot from a JSON file.
func loadSnapshot(path string) (s *indexSnapshot, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	var j indexSnapshotJSON
	if err = json.NewDecoder(f).Decode(&j); err != nil {
		return nil, err
	}
	return fromJSON(&j), nil
}
