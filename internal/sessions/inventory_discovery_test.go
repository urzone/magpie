package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInventoryReusesCheckedHermesFiles(t *testing.T) {
	setup(t)
	root := filepath.Join(t.TempDir(), "hermes")
	t.Setenv("HERMES_HOME", root)
	path := hermesFixture(t, root, "", "kept", false)
	dbReadMu.Lock()
	defer dbReadMu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	files, err := hermesFilesChecked()
	if err != nil || len(files) != 1 {
		t.Fatalf("initial discovery: %v, %v", files, err)
	}
	closeDBs()
	// An unchecked rediscovery would now fail and lose the native identity.
	if err := os.WriteFile(path, []byte("not SQLite anymore"), 0600); err != nil {
		t.Fatal(err)
	}
	got := allFilesWithHermes(files)
	defer closeDBs()
	kept := false
	for _, f := range got {
		if f.agent == "hermes" && f.sid == "kept" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("checked inventory was replaced by another discovery")
	}
}
