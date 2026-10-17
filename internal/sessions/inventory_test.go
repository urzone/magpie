package sessions

import "testing"

// Counts and gateway/native dedup need identity, not transcript statistics.
func TestInventoryDoesNotParseSessions(t *testing.T) {
	setup(t)
	calls := 0
	parseHook = func() { calls++ }
	t.Cleanup(func() { parseHook = nil })
	counts, ids := Inventory()
	if calls != 0 {
		t.Fatalf("identity enumeration parsed %d transcripts", calls)
	}
	if len(counts) != 2 {
		t.Fatalf("agent counts: %+v", counts)
	}
	if !ids["claude"]["11111111-2222-3333-4444-555555555555"] {
		t.Fatal("missing native Claude identity")
	}
	if len(ids["codex"]) == 0 {
		t.Fatal("missing native Codex identities")
	}
	for _, a := range counts {
		if a.Count != len(ids[a.Agent]) {
			t.Fatalf("count differs from identities for %s", a.Agent)
		}
	}
}
