package lib

// Helper doubles its input.
func Helper(x int) int {
	return x * 2
}

// internalHelper is intentionally unused — it exists to exercise dead-code
// detection in the analysis test suite.
func internalHelper(s string) string {
	return "processed: " + s
}

// Data is an exported type with a method.
type Data struct {
	Value string
}

// Process returns a transformed version of Value.
func (d Data) Process() string {
	return "result: " + d.Value
}

// unusedType is intentionally unused — it exists to exercise dead-code
// detection in the analysis test suite.
type unusedType struct {
	X int
}

// DoNothing is a method on the intentionally unused type.
func (u unusedType) DoNothing() {}
