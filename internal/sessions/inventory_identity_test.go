package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestInventoryAuthoritativeIdentities(t *testing.T) {
	d := setupCalls(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	codexIdentityRollout(t, d.codex, "file-thread", codexIdentityMeta(t, "own-thread", "shared", `"cli"`, "", ""))
	// Same filename key, later segment: only the earliest segment names the row.
	writeLines(t, filepath.Join(d.codex, "sessions", "rollout-2026-09-21T10-00-00-file-thread.jsonl"), codexIdentityMeta(t, "later-thread", "shared", `"cli"`, "", ""))
	writeLines(t, filepath.Join(PiDir(), "sessions", "project", "2026_file-pi.jsonl"), `{"type":"session","id":"own-pi"}`)
	omp := filepath.Join(OmpDir(), "sessions", "project", "2026_file-omp")
	writeLines(t, omp+".jsonl", `{"type":"title","title":"padding"}`, `{"type":"session","id":"own-omp"}`)
	writeLines(t, filepath.Join(omp, "a-child.jsonl"), `{"type":"session","id":"child-omp"}`)
	grok := filepath.Join(GrokDir(), "sessions", "project", "file-grok")
	writeLines(t, filepath.Join(grok, "updates.jsonl"), `{}`)
	writeLines(t, filepath.Join(grok, "summary.json"), `{"info":{"id":"own-grok"}}`)
	child := filepath.Join(GrokDir(), "sessions", "project", "child-grok")
	writeLines(t, filepath.Join(child, "updates.jsonl"), `{}`)
	writeLines(t, filepath.Join(child, "summary.json"), `{"info":{"id":"child-grok"},"session_kind":"subagent","parent_session_id":"file-grok"}`)
	writeLines(t, filepath.Join(FactoryDir(), "sessions", "file-droid.jsonl"), `{"type":"session_start","id":"own-droid"}`)
	writeLines(t, filepath.Join(FactoryDir(), "sessions", "child-droid.jsonl"), `{"type":"session_start","id":"child-droid","callingSessionId":"file-droid"}`)
	writeLines(t, filepath.Join(DshDir(), "sessions", "project", "own-dsh", "session.v4.jsonl"), `{"type":"session","version":4,"id":"own-dsh"}`)
	writeLines(t, filepath.Join(DshDir(), "sessions", "project", "child-dsh", "session.v4.jsonl"), `{"type":"session","version":4,"id":"child-dsh","delegationDepth":1,"parentSession":"own-dsh"}`)
	parsed := 0
	parseHook = func() { parsed++ }
	t.Cleanup(func() { parseHook = nil })
	counts, ids := Inventory()
	if parsed != 0 {
		t.Fatalf("inventory parsed %d transcripts", parsed)
	}
	want := map[string]map[string]bool{}
	for _, a := range []string{"codex", "pi", "omp", "grok", "droid", "dsh"} {
		want[a] = map[string]bool{"own-" + a: true}
	}
	want["codex"] = map[string]bool{"own-thread": true}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("inventory identities = %v, want %v", ids, want)
	}
	for _, c := range counts {
		if c.Count != 1 {
			t.Errorf("count = %+v", c)
		}
	}
	parseHook = nil
	for agent, keys := range ids {
		rows := ListAgent(agent)
		if len(rows) != 1 || !keys[rows[0].ID] {
			t.Fatalf("%s native rows disagree with inventory: %+v", agent, rows)
		}
	}
	// Exercise the public delete lookup using precisely the inventory's ID.
	old := time.Now().Add(-24 * time.Hour)
	for _, f := range codexFiles() {
		if err := os.Chtimes(f.path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Delete("codex", "file-thread"); err == nil {
		t.Fatal("filename alias unexpectedly deleted native session")
	}
	if _, err := Delete("codex", "own-thread"); err != nil {
		t.Fatalf("authoritative ID delete: %v", err)
	}
}

func TestInventoryColdWSLUsesSavedIdentity(t *testing.T) {
	d := setupCalls(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	path := codexIdentityRollout(t, d.codex, "file-thread", codexIdentityMeta(t, "own-thread", "shared", `"cli"`, "", ""))
	rows := ListAgent("codex")
	if len(rows) != 1 || rows[0].ID != "own-thread" {
		t.Fatal(rows)
	}
	Saved()
	Reset()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	WSLHomes = func() []WSLHome { return []WSLHome{{Distro: "Synthetic", Running: false}} }
	WSLRunning = func(string) bool { return false }
	t.Cleanup(func() { wslSettle(); WSLHomes = nil; WSLRunning = nil; Reset() })
	wslSess.Lock()
	wslSess.loaded = true
	wslSess.at = time.Now()
	wslSess.lists = map[string]*wslListing{"Synthetic": {Files: []wslEntry{{Agent: "codex", Key: "codex:file-thread", Path: path, Main: true}}}}
	wslSess.Unlock()
	scanHook = func(p string, _ int64) { t.Errorf("cold WSL opened %s", filepath.Base(p)) }
	t.Cleanup(func() { scanHook = nil })
	_, ids := Inventory()
	if !ids["codex"]["own-thread"] || ids["codex"]["file-thread"] {
		t.Fatalf("saved WSL identity = %v", ids)
	}
}

func TestInventoryPackedAndMissingFirstIdentity(t *testing.T) {
	d := setupCalls(t)
	p := codexIdentityRollout(t, d.codex, "packed-file", codexIdentityMeta(t, "packed-native", "shared", `"cli"`, "", ""))
	pack(t, p, false)
	_, ids := Inventory()
	if !ids["codex"]["packed-native"] || ids["codex"]["packed-file"] {
		t.Fatalf("packed identities: %v", ids)
	}
	// An empty earliest segment keeps the grouping key: a later main file
	// must not supply its ID, just as assembleStates refuses that override.
	a := filepath.Join(t.TempDir(), "a.jsonl")
	b := filepath.Join(t.TempDir(), "b.jsonl")
	writeLines(t, a, `{"type":"message"}`)
	writeLines(t, b, `{"type":"session","id":"later"}`)
	fs := []file{{agent: "omp", key: "omp:file", path: b, main: true}, {agent: "omp", key: "omp:file", path: a, main: true}}
	if got := inventoryIdentity(fs); got != "file" {
		t.Fatalf("later main overrode empty first segment: %q", got)
	}
}

func TestInventoryGroupPrecedence(t *testing.T) {
	setupCalls(t)
	for _, tc := range []struct {
		name   string
		main   bool
		header string
		want   string
	}{
		{"main", true, `{"type":"session","id":"native"}`, "native"},
		{"missing-header", true, `{"type":"message"}`, "file"},
		{"orphan-subagent", false, `{"type":"session","id":"child"}`, "file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "session.jsonl")
			writeLines(t, p, tc.header)
			f := file{agent: "omp", key: "omp:file", path: p, main: tc.main}
			got := inventoryIdentity([]file{f})
			if got != tc.want {
				t.Fatalf("identity %q, want %q", got, tc.want)
			}
		})
	}
}
