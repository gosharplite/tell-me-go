package main

import (
	"fmt"

	"example.com/analysis-fixture/lib"
)

func main() {
	fmt.Println(lib.Helper(5))

	// Reference SimpleRunner so the type is not flagged as dead code.
	var _ lib.Runner = lib.SimpleRunner{}
}
