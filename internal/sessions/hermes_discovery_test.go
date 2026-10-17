package sessions

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestHermesManagementRejectsUnknownDiscovery(t *testing.T) {
	for _, kind := range []string{"corrupt", "missing-id", "scan", "directory", "profiles-file", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			setup(t)
			root := filepath.Join(t.TempDir(), "hermes")
			t.Setenv("HERMES_HOME", root)
			if err := os.MkdirAll(root, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "state.db")
			switch kind {
			case "corrupt":
				if err := os.WriteFile(path, []byte("not a SQLite database"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				path = hermesFixture(t, root, "", "denied", false)
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0600) })
				if f, err := os.Open(path); err == nil {
					f.Close()
					t.Skip("user bypasses file permissions")
				}
			case "directory":
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
			case "profiles-file":
				if err := os.WriteFile(filepath.Join(root, "profiles"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				schema := `CREATE TABLE sessions (title TEXT)`
				if kind == "scan" {
					schema = `CREATE TABLE sessions (id TEXT, started_at TEXT); INSERT INTO sessions VALUES ('bad', 'invalid timestamp')`
				}
				_, err = db.Exec(schema)
				db.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := ListAgentView("hermes")
			if err == nil {
				t.Fatalf("unknown Hermes discovery reported success: %+v", got)
			}
			if len(got) != 0 {
				t.Fatalf("partial discovery returned alongside error: %+v", got)
			}
			counts, ids, err := InventoryChecked()
			if err == nil || len(counts) != 0 || len(ids) != 0 {
				t.Fatal("management inventory treated unknown discovery as complete")
			}
		})
	}
}

func TestHermesManagementMissingAndOldStores(t *testing.T) {
	setup(t)
	root := filepath.Join(t.TempDir(), "hermes")
	t.Setenv("HERMES_HOME", root)
	got, err := ListAgentView("hermes")
	if err != nil || len(got) != 0 {
		t.Fatalf("missing store: %+v, %v", got, err)
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE sessions (id TEXT, title TEXT); INSERT INTO sessions VALUES ('old','Old schema')`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	got, err = ListAgentView("hermes")
	if err != nil || len(got) != 1 || got[0].Title != "Old schema" {
		t.Fatalf("old optional tables/columns: %+v, %v", got, err)
	}
}
