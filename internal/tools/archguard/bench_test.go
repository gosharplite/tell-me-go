package archguard

import (
	"context"
	"testing"
)

func BenchmarkAnalyze(b *testing.B) {
	ctx := context.Background()
	rootPath := "./..."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Analyze(ctx, rootPath)
		if err != nil {
			b.Fatal(err)
		}
	}
}
