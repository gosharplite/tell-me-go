package analysis

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestASTCache_Concurrency_Race(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cache := newASTCache(tmpDir)
	numFiles := 10
	files := make([]string, numFiles)
	for i := 0; i < numFiles; i++ {
		path := fmt.Sprintf("file%d.go", i)
		if err := os.WriteFile(filepath.Join(tmpDir, path), []byte(fmt.Sprintf("package p%d\nfunc F%d() {}\n", i, i)), 0644); err != nil {
			t.Fatal(err)
		}
		files[i] = path
	}

	var wg sync.WaitGroup
	numG := 50
	start := make(chan struct{})

	for i := 0; i < numG; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < 20; j++ {
				path := files[(id+j)%numFiles]
				_, _, err := cache.Get(path)
				if err != nil {
					t.Errorf("Get(%s) error: %v", path, err)
				}
			}
		}(i)
	}

	close(start)
	wg.Wait()
}
