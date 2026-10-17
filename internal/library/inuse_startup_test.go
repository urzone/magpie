package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The desktop's parsing-blocking boot.js needs this count while startup
// synchronization holds mu. Reading the committed metadata must not wait
// for synchronization, even with an empty library.
func TestInUseDoesNotWaitForSync(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	if err := os.MkdirAll(filepath.Dir(path()), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"empty", `{}`, 0},
		{"items", `{"mcp":[{}],"skills":[{},{}],"hooks":[{}],"projects":[{}],"instructions":{"agents":["codex"]}}`, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path(), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			done := make(chan struct{})
			var n int
			var err error
			go func() { n, err = InUse(); close(done) }()
			select {
			case <-done:
				mu.Unlock()
				if err != nil || n != tc.want {
					t.Fatalf("count: %d, %v; want %d", n, err, tc.want)
				}
			case <-time.After(2 * time.Second):
				mu.Unlock()
				<-done
				t.Fatal("plugin bootstrap count waited for the library synchronization lock")
			}
		})
	}
}

func TestInUseInvalidMetadataIsUnknown(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	if n, err := InUse(); err != nil || n != 0 {
		t.Fatalf("missing library: %d, %v", n, err)
	}
	if err := os.MkdirAll(filepath.Dir(path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path(), []byte(`{"skills": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InUse(); err == nil {
		t.Fatal("invalid metadata was treated as an empty library")
	}
	if err := os.WriteFile(path(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InUse(); err == nil {
		t.Fatal("existing zero-byte metadata was treated as a missing library")
	}
	if err := os.Remove(path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := InUse(); err == nil {
		t.Fatal("unreadable metadata was treated as an empty library")
	}
}
