package auth

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadOrCreateUIDConcurrentCreationIsStable(t *testing.T) {
	dir := t.TempDir()
	const attempts = 16
	results := make(chan string, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			uid, err := LoadOrCreateUID(dir)
			results <- uid
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for uid := range results {
		if first == "" {
			first = uid
		}
		if uid != first {
			t.Fatalf("concurrent identity mismatch: %q != %q", uid, first)
		}
	}
}

func TestLoadOrCreateUIDRejectsMalformedExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uid")
	if err := os.WriteFile(path, []byte("corrupt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateUID(dir); err == nil {
		t.Fatal("malformed UID was silently replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "corrupt\n" {
		t.Fatalf("malformed identity was modified: %q", data)
	}
}

func TestLoadOrCreateUIDRepairsPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uid")
	if err := os.WriteFile(path, []byte("00112233445566778899aabbccddeeff\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateUID(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}
