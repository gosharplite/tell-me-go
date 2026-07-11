package jsoncontracts
// Payload satisfies json.Marshaler / json.Unmarshaler.
type Payload struct{}
func (p Payload) MarshalJSON() ([]byte, error)   { return []byte("{}"), nil }
func (p *Payload) UnmarshalJSON(data []byte) error { return nil }
