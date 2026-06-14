// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/types"
)

// wellKnownContractSignature describes the parameter and result types of a single
// stdlib interface method, in canonical (*types.Type).String() form (parameter
// names are intentionally omitted).
//
// Type strings were captured empirically by loading the stdlib via
// golang.org/x/tools/go/packages and printing each Param/Result type's
// .String() value. See the original probe results for reference:
//
//	io.Reader.Read       params=[[]byte] results=[int error]
//	io.Writer.Write      params=[[]byte] results=[int error]
//	io.Closer.Close      params=[]       results=[error]
//	io.Seeker.Seek       params=[int64 int] results=[int64 error]
//	io.WriterTo.WriteTo  params=[io.Writer] results=[int64 error]
//	io.ReaderFrom.ReadFrom params=[io.Reader] results=[int64 error]
//	fmt.Stringer.String  params=[] results=[string]
//	fmt.Formatter.Format params=[fmt.State rune] results=[]
//	json.Marshaler.MarshalJSON   params=[] results=[[]byte error]
//	json.Unmarshaler.UnmarshalJSON params=[[]byte] results=[error]
//	encoding.TextMarshaler.MarshalText     params=[] results=[[]byte error]
//	encoding.TextUnmarshaler.UnmarshalText params=[[]byte] results=[error]
//	encoding.BinaryMarshaler.MarshalBinary     params=[] results=[[]byte error]
//	encoding.BinaryUnmarshaler.UnmarshalBinary params=[[]byte] results=[error]
//	http.Handler.ServeHTTP params=[net/http.ResponseWriter *net/http.Request] results=[]
//	sql.Scanner.Scan       params=[any] results=[error]
//
// Note: "any" and "interface{}" canonicalize to different strings via
// (*types.Type).String() depending on whether the implementor used the
// modern alias or the literal empty interface. canonicalTypeString
// normalizes both to "any".
//
// Decisions for this iteration:
//   - sort.Interface (Len/Less/Swap) is intentionally omitted: those names are
//     too common in user code, so protecting them in isolation would create
//     more false negatives than the false positives it removes. Future work
//     could match the triple structurally on a single receiver.
//   - driver.Valuer (Value() (driver.Value, error)) is omitted because
//     driver.Value is `any`, making the signature ambiguous with many
//     unrelated user methods.
type wellKnownContractSignature struct {
	params  []string
	results []string
}

// wellKnownContractMethods enumerates stdlib single-method interfaces that may
// be satisfied structurally. A method on a user type whose name appears here
// AND whose signature matches one of the listed shapes is protected from
// being flagged as DEAD/PRIVATE, since it can be invoked through the
// corresponding stdlib interface without the method's name appearing at any
// call site visible to static analysis.
//
// A name maps to a slice of shapes because some method names belong to
// multiple stdlib contracts (e.g., MarshalText and MarshalJSON share a
// shape but have distinct names; future contracts may overload further).
var wellKnownContractMethods = map[string][]wellKnownContractSignature{
	// error.Error
	"Error": {{params: nil, results: []string{"string"}}},
	// fmt.Stringer.String
	"String": {{params: nil, results: []string{"string"}}},
	// io.Reader.Read
	"Read": {{params: []string{"[]byte"}, results: []string{"int", "error"}}},
	// io.Writer.Write
	"Write": {{params: []string{"[]byte"}, results: []string{"int", "error"}}},
	// io.Closer.Close
	"Close": {{params: nil, results: []string{"error"}}},
	// io.Seeker.Seek
	"Seek": {{params: []string{"int64", "int"}, results: []string{"int64", "error"}}},
	// io.WriterTo.WriteTo
	"WriteTo": {{params: []string{"io.Writer"}, results: []string{"int64", "error"}}},
	// io.ReaderFrom.ReadFrom
	"ReadFrom": {{params: []string{"io.Reader"}, results: []string{"int64", "error"}}},
	// fmt.Formatter.Format
	"Format": {{params: []string{"fmt.State", "rune"}, results: nil}},
	// encoding/json.Marshaler.MarshalJSON
	"MarshalJSON": {{params: nil, results: []string{"[]byte", "error"}}},
	// encoding/json.Unmarshaler.UnmarshalJSON
	"UnmarshalJSON": {{params: []string{"[]byte"}, results: []string{"error"}}},
	// encoding.TextMarshaler.MarshalText
	"MarshalText": {{params: nil, results: []string{"[]byte", "error"}}},
	// encoding.TextUnmarshaler.UnmarshalText
	"UnmarshalText": {{params: []string{"[]byte"}, results: []string{"error"}}},
	// encoding.BinaryMarshaler.MarshalBinary
	"MarshalBinary": {{params: nil, results: []string{"[]byte", "error"}}},
	// encoding.BinaryUnmarshaler.UnmarshalBinary
	"UnmarshalBinary": {{params: []string{"[]byte"}, results: []string{"error"}}},
	// net/http.Handler.ServeHTTP
	"ServeHTTP": {{params: []string{"net/http.ResponseWriter", "*net/http.Request"}, results: nil}},
	// database/sql.Scanner.Scan
	"Scan": {{params: []string{"any"}, results: []string{"error"}}},
}

// canonicalTypeString returns t.String() with two stdlib-alias normalizations
// applied:
//
//  1. `interface{}` → `any`. The empty-interface alias has two spellings; both
//     satisfy database/sql.Scanner.Scan, so we collapse them.
//  2. `[]uint8` → `[]byte`. `byte` is an alias for `uint8`, but real Go source
//     loaded via go/packages prints byte slices as `[]byte` while
//     programmatically-constructed slices print as `[]uint8`. Both denote the
//     same underlying type and must compare equal here.
func canonicalTypeString(t types.Type) string {
	s := t.String()
	switch s {
	case "interface{}":
		return "any"
	case "[]uint8":
		return "[]byte"
	}
	return s
}

// signatureMatches reports whether sig has exactly the given parameter and
// result type strings (per canonicalTypeString).
func signatureMatches(sig *types.Signature, want wellKnownContractSignature) bool {
	if sig.Params().Len() != len(want.params) {
		return false
	}
	if sig.Results().Len() != len(want.results) {
		return false
	}
	for i, p := range want.params {
		if canonicalTypeString(sig.Params().At(i).Type()) != p {
			return false
		}
	}
	for i, r := range want.results {
		if canonicalTypeString(sig.Results().At(i).Type()) != r {
			return false
		}
	}
	return true
}

// isWellKnownContract reports whether obj is a method that may be invoked
// structurally through a stdlib interface (e.g., io.Writer, fmt.Stringer,
// http.Handler). Such methods must be protected from being flagged as
// DEAD/PRIVATE because the call site never names them.
func (a *defaultDeadCodeAnalyzer) isWellKnownContract(obj types.Object) bool {
	// Only *types.Func objects can match a well-known contract signature.
	// Two distinct failure modes return false silently:
	//   1. Not a function (variable, type, constant)
	//   2. Function with a non-signature type (builtin, invalid)
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	shapes, ok := wellKnownContractMethods[fn.Name()]
	if !ok {
		return false
	}
	for _, shape := range shapes {
		if signatureMatches(sig, shape) {
			return true
		}
	}
	return false
}
