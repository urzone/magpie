package sessions

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type hermesNoBodyQuery struct {
	hermesQuery
	t       *testing.T
	queries *[]string
}

func (q hermesNoBodyQuery) check(query string) error {
	*q.queries = append(*q.queries, query)
	if strings.Contains(strings.ToLower(query), "messages") {
		q.t.Errorf("named management session queried message rows: %s", query)
		return fmt.Errorf("management message query forbidden")
	}
	return nil
}

func (q hermesNoBodyQuery) Query(query string, args ...any) (*sql.Rows, error) {
	if err := q.check(query); err != nil {
		return nil, err
	}
	return q.hermesQuery.Query(query, args...)
}

func (q hermesNoBodyQuery) QueryRow(query string, args ...any) *sql.Row {
	// QueryRow defers errors to Scan; marking the test failed here also catches
	// a caller that ignores the row error.
	if q.check(query) != nil {
		query = "SELECT * FROM management_body_query_forbidden"
	}
	return q.hermesQuery.QueryRow(query, args...)
}

func TestHermesManagedUsesCanonicalUsageWithoutTranscript(t *testing.T) {
	setup(t)
	root := filepath.Join(t.TempDir(), "hermes")
	t.Setenv("HERMES_HOME", root)
	p := hermesFixture(t, root, "", "summary", true)
	full := ListAgent("hermes")
	if len(full) != 1 {
		t.Fatalf("full: %d", len(full))
	}
	oldQuery := hermesManagementQuery
	var queries []string
	hermesManagementQuery = func(tx *sql.Tx) hermesQuery {
		return hermesNoBodyQuery{hermesQuery: tx, t: t, queries: &queries}
	}
	t.Cleanup(func() { hermesManagementQuery = oldQuery })
	got, err := ListAgentView("hermes")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("view: %d", len(got))
	}
	if len(queries) < 2 {
		t.Fatalf("management SQL was not observed: %v", queries)
	}
	if got[0].Title != full[0].Title || got[0].Cwd != full[0].Cwd || got[0].Tokens != full[0].Tokens || !reflect.DeepEqual(got[0].Models, full[0].Models) || got[0].Cost != full[0].Cost {
		t.Fatalf("metadata/usage differs: %+v / %+v", got[0], full[0])
	}
	if !got[0].DetailsPending || got[0].Deletable {
		t.Fatal("missing detail/read-only contract")
	}
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Same last_seen and original message times; aggregates and title still refresh.
	if _, err = db.Exec(`UPDATE sessions SET title='changed'; UPDATE session_model_usage SET input_tokens = input_tokens + 17`); err != nil {
		t.Fatal(err)
	}
	next, err := ListAgentView("hermes")
	if err != nil {
		t.Fatal(err)
	}
	if next[0].Title != "changed" || next[0].Input != got[0].Input+17 {
		t.Fatalf("metadata update stale: %+v", next[0])
	}
}
