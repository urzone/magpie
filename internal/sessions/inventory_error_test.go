package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckedIdentityReadFailureIsNotFilenameFallback(t *testing.T) {
	setupCalls(t)
	p := filepath.Join(t.TempDir(), "unreadable.jsonl")
	if err := os.Mkdir(p, 0700); err != nil {
		t.Fatal(err)
	}
	f := file{agent: "codex", key: "codex:filename-alias", path: p, main: true}
	if id, err := inventoryIdentityChecked([]file{f}); err == nil {
		t.Fatalf("failed native identity became %q", id)
	}
}

func TestCheckedIdentityCorruptPackedHeaderIsUnknown(t *testing.T) {
	setupCalls(t)
	p := filepath.Join(t.TempDir(), "rollout.jsonl.zst")
	if err := os.WriteFile(p, []byte("not a zstd frame"), 0600); err != nil {
		t.Fatal(err)
	}
	f := file{agent: "codex", key: "codex:filename-alias", path: p, main: true}
	if id, err := inventoryIdentityChecked([]file{f}); err == nil {
		t.Fatalf("failed decompression became %q", id)
	}
}
