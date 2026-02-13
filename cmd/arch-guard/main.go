package main

import (
	"context"
	"fmt"
	"log"

	"github.com/gosharplite/tell-me-go/internal/tools/archguard"
)

func main() {
	fmt.Println("Loading packages...")
	findings, err := archguard.Analyze(context.Background(), "./...")
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range findings {
		if f.Reason != "" {
			fmt.Printf("[%s] %s (%s)\n", f.Category, f.Symbol, f.Reason)
		} else {
			fmt.Printf("[%s] %s\n", f.Category, f.Symbol)
		}
	}
}
