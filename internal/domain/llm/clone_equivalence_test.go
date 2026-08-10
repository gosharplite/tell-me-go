// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"bytes"
	"reflect"
	"testing"
)

// referenceCloneSlice is the pre-arena reference implementation: per-entry
// CloneContent, exactly what GetWindow did before the arena path. Every
// arena assertion in this file is checked against it.
func referenceCloneSlice(src []*Content) []*Content {
	out := make([]*Content, len(src))
	for i, c := range src {
		out[i] = CloneContent(c)
	}
	return out
}

// corpusEntries returns the edge corpus covering every structure the clone
// must handle: nil entries, nil parts, nil transient parts, nil
// InlineData/FunctionCall/FunctionResponse, empty and populated slices,
// empty and nested maps/slices, mixed interface{} values, thought
// signatures, pinned entries and populated token counts.
func corpusEntries() []*Content {
	return []*Content{
		nil, // nil Content
		{Role: "user", ID: "u1", Parts: []*Part{{Text: "hello"}}},      // basic text part
		{Role: "model", ID: "m1"},                                      // nil Parts, nil TransientParts
		{Role: "user", ID: "u2", Parts: []*Part{}},                     // empty non-nil Parts
		{Role: "user", ID: "u3", TransientParts: []*Part{}},            // empty non-nil TransientParts
		{Role: "user", ID: "u4", Parts: []*Part{nil}},                  // nil Part inside Parts
		{Role: "model", ID: "m2", Parts: []*Part{                       // thought part + non-empty signature
			{Text: "thinking", IsThought: true, ThoughtSignature: []byte{0x01, 0x02, 0x03}},
		}},
		{Role: "user", ID: "u5", TokenCount: 123, Pinned: true,         // populated TokenCount + Pinned
			Parts: []*Part{{Text: "pinned text"}}},
		{Role: "user", ID: "u6", Parts: []*Part{                        // empty Args/Response maps
			{FunctionCall: &FunctionCall{ID: "fc1", Name: "tool", Args: map[string]interface{}{}}},
			{FunctionResponse: &FunctionResponse{ID: "fr1", Name: "tool", Response: map[string]interface{}{}}},
		}},
		{Role: "user", ID: "u7", Parts: []*Part{{FunctionCall: &FunctionCall{ // nested maps + nested slices + mixed values
			ID:   "fc2",
			Name: "tool",
			Args: map[string]interface{}{
				"nested": map[string]interface{}{
					"a": []interface{}{"x", float64(1), true, nil},
					"b": "y",
				},
			},
		}}}},
		{Role: "user", ID: "u8", Parts: []*Part{{InlineData: &Blob{ // blob with Data bytes
			MIMEType: "image/png",
			Data:     []byte{0x89, 0x50, 0x4E, 0x47},
		}}}},
		{Role: "user", ID: "u9", Parts: []*Part{{Text: "t1"}},         // populated TransientParts alongside Parts
			TransientParts: []*Part{{Text: "tp", IsThought: true}}},
		{Role: "model", ID: "m3", Parts: []*Part{{FunctionResponse: &FunctionResponse{ // nested Response map
			ID:       "fr2",
			Name:     "r",
			Response: map[string]interface{}{"k": map[string]interface{}{"deep": []interface{}{"v"}}},
		}}}},
		{Role: "user", ID: "u10", Parts: []*Part{ // multi-part entry
			{Text: "a"}, {Text: "b"}, {FunctionCall: &FunctionCall{ID: "fc3", Name: "t"}},
		}},
	}
}

// TestCloneArena_EquivalenceWithCloneContent asserts the arena clone is
// IDENTICAL to CloneContent's output via reflect.DeepEqual (stronger than
// EqualContent: covers TokenCount, Pinned, ID and ThoughtSignature bytes)
// for the full edge corpus, including slice length/order and nil mapping.
func TestCloneArena_EquivalenceWithCloneContent(t *testing.T) {
	corpus := corpusEntries()

	arena := NewCloneArena(len(corpus))
	got := arena.CloneContentSlice(corpus)
	want := referenceCloneSlice(corpus)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arena clone differs from CloneContent reference:\ngot:  %#v\nwant: %#v", got, want)
	}
	if len(got) != len(corpus) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(corpus))
	}
	for i, c := range corpus {
		if c == nil {
			if got[i] != nil {
				t.Errorf("entry %d: nil source mapped to non-nil clone", i)
			}
			continue
		}
		if got[i] == nil {
			t.Errorf("entry %d: non-nil source mapped to nil clone", i)
		}
	}
}

// TestCloneArena_SingleEntryEquivalence exercises the entry-level
// CloneContent method on the arena against CloneContent for representative
// entries (text, tool-call, transient, thought-signature).
func TestCloneArena_SingleEntryEquivalence(t *testing.T) {
	entries := []*Content{
		{Role: "user", ID: "s1", Parts: []*Part{{Text: "single"}}},
		{Role: "user", ID: "s2", Parts: []*Part{{FunctionCall: &FunctionCall{
			ID: "fc", Name: "t", Args: map[string]interface{}{"k": []interface{}{"v", float64(2)}},
		}}}},
		{Role: "model", ID: "s3", Parts: []*Part{{IsThought: true, ThoughtSignature: []byte{9, 8, 7}}},
			TransientParts: []*Part{{Text: "tp"}}},
	}
	for _, e := range entries {
		arena := NewCloneArena(1)
		got := arena.CloneContent(e)
		want := CloneContent(e)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("single-entry arena clone differs for %q:\ngot:  %#v\nwant: %#v", e.ID, got, want)
		}
	}
}

// deepMutate mutates every mutable field of c in place (strings are
// replaced — immutable headers; []byte and map values are mutated in
// place to catch aliasing).
func deepMutate(c *Content) {
	if c == nil {
		return
	}
	c.Role = "mutated"
	c.ID = "mutated-id"
	c.TokenCount = 999
	c.Pinned = !c.Pinned
	for _, p := range c.Parts {
		if p == nil {
			continue
		}
		p.Text = "mutated-text"
		p.IsThought = !p.IsThought
		p.AssetID = "mutated-asset"
		if len(p.ThoughtSignature) > 0 {
			p.ThoughtSignature[0] ^= 0xFF
		}
		if p.InlineData != nil && len(p.InlineData.Data) > 0 {
			p.InlineData.Data[0] ^= 0xFF
		}
		if p.FunctionCall != nil && len(p.FunctionCall.Args) > 0 {
			for k := range p.FunctionCall.Args {
				p.FunctionCall.Args[k] = "mutated-arg"
			}
		}
		if p.FunctionResponse != nil && len(p.FunctionResponse.Response) > 0 {
			for k := range p.FunctionResponse.Response {
				p.FunctionResponse.Response[k] = "mutated-resp"
			}
		}
	}
	for _, p := range c.TransientParts {
		if p == nil {
			continue
		}
		p.Text = "mutated-transient"
	}
}

// TestCloneArena_MutationIsolation asserts full ownership transfer in both
// directions for every corpus entry: mutating the source leaves the clone
// unchanged, and mutating the clone leaves the source unchanged.
func TestCloneArena_MutationIsolation(t *testing.T) {
	for idx, src := range corpusEntries() {
		if src == nil {
			continue
		}
		arena := NewCloneArena(1)
		clone := arena.CloneContent(src)
		wantClone := CloneContent(src) // snapshot before any mutation

		// Direction 1: mutate source -> clone unchanged.
		deepMutate(src)
		if !reflect.DeepEqual(clone, wantClone) {
			t.Errorf("entry %d: source mutation leaked into arena clone", idx)
		}
		// Restore the source for direction 2 by re-cloning the pristine
		// reference (the arena clone is still pristine).
		src = CloneContent(wantClone)

		// Direction 2: mutate clone -> source unchanged.
		snapshotSrc := CloneContent(src)
		deepMutate(clone)
		if !reflect.DeepEqual(src, snapshotSrc) {
			t.Errorf("entry %d: arena-clone mutation leaked into source", idx)
		}
	}
}

// TestCloneArena_AppendSafety asserts the arena's full-slice-expression
// sub-slices behave like standalone allocations: appending to one clone's
// Parts never clobbers a neighbour's slots or the source.
func TestCloneArena_AppendSafety(t *testing.T) {
	src := []*Content{
		{Role: "user", ID: "a", Parts: []*Part{{Text: "a0"}}},
		{Role: "model", ID: "b", Parts: []*Part{{Text: "b0"}}},
		{Role: "user", ID: "c", Parts: []*Part{{Text: "c0"}}},
	}
	arena := NewCloneArena(len(src))
	clones := arena.CloneContentSlice(src)

	// Snapshot neighbour and source before the append.
	neighbourBefore := CloneContent(clones[1])
	sourceBefore := CloneContent(src[0])

	// Append to clone[0].Parts: with cap == len on the sub-slice this must
	// reallocate, leaving clone[1] and src[0] untouched.
	clones[0].Parts = append(clones[0].Parts, &Part{Text: "appended"})

	if !reflect.DeepEqual(clones[1], neighbourBefore) {
		t.Error("append to clone[0].Parts clobbered clone[1].Parts (shared backing aliasing)")
	}
	if !reflect.DeepEqual(src[0], sourceBefore) {
		t.Error("append to clone[0].Parts leaked into the source")
	}
	if len(clones[0].Parts) != 2 || clones[0].Parts[1].Text != "appended" {
		t.Errorf("append result unexpected: %#v", clones[0].Parts)
	}
}

// TestCloneArena_EmptyAndNilInputs covers degenerate slice inputs.
func TestCloneArena_EmptyAndNilInputs(t *testing.T) {
	var nilSrc []*Content
	emptySrc := []*Content{}

	arenaNil := NewCloneArena(0)
	gotNil := arenaNil.CloneContentSlice(nilSrc)
	if gotNil == nil || len(gotNil) != 0 {
		t.Errorf("nil input: got %#v, want empty non-nil slice", gotNil)
	}
	if !reflect.DeepEqual(gotNil, referenceCloneSlice(nilSrc)) {
		t.Error("nil input differs from reference")
	}

	arenaEmpty := NewCloneArena(0)
	gotEmpty := arenaEmpty.CloneContentSlice(emptySrc)
	if gotEmpty == nil || len(gotEmpty) != 0 {
		t.Errorf("empty input: got %#v, want empty non-nil slice", gotEmpty)
	}
	if !reflect.DeepEqual(gotEmpty, referenceCloneSlice(emptySrc)) {
		t.Error("empty input differs from reference")
	}
}

// TestCloneArena_ThoughtSignatureAndBlobBytes asserts the []byte copies
// (ThoughtSignature, Blob.Data) are truly independent byte-for-byte.
func TestCloneArena_ThoughtSignatureAndBlobBytes(t *testing.T) {
	sig := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	blobData := []byte{1, 2, 3, 4}
	src := []*Content{{
		Role: "model",
		ID:   "sig",
		Parts: []*Part{
			{IsThought: true, ThoughtSignature: sig},
			{InlineData: &Blob{MIMEType: "application/octet-stream", Data: blobData}},
		},
	}}

	arena := NewCloneArena(1)
	clone := arena.CloneContentSlice(src)[0]

	// Mutate the source bytes in place.
	sig[0] = 0x00
	blobData[0] = 0x00

	if clone.Parts[0].ThoughtSignature[0] != 0xDE {
		t.Errorf("ThoughtSignature aliases source: got %x", clone.Parts[0].ThoughtSignature[0])
	}
	if clone.Parts[1].InlineData.Data[0] != 1 {
		t.Errorf("Blob.Data aliases source: got %x", clone.Parts[1].InlineData.Data[0])
	}
	if !bytes.Equal(clone.Parts[0].ThoughtSignature, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("ThoughtSignature corrupted: %x", clone.Parts[0].ThoughtSignature)
	}
	if !bytes.Equal(clone.Parts[1].InlineData.Data, []byte{1, 2, 3, 4}) {
		t.Errorf("Blob.Data corrupted: %x", clone.Parts[1].InlineData.Data)
	}
}
