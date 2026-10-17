package sessions

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

// Preserve the pre-projection collector as an independent equivalence oracle.
func legacyHermesProjection(h *hermesDB, q hermesQuery, sid string, fn func(hermesMessage)) error {
	if !h.messages["session_id"] || !h.messages["role"] {
		return nil
	}
	// Match Hermes's display identity, including NULL versus empty tool fields.
	// Content alone would collapse legitimate repeated turns and tool results.
	type identity struct {
		role, content, tools, toolID, toolName sql.NullString
		at                                     float64
	}
	column := func(name string) string {
		if h.messages[name] {
			return name
		}
		return "NULL"
	}
	query := "SELECT " + strings.Join([]string{
		column("role"), column("content"), column("tool_calls"),
		column("tool_call_id"), column("tool_name"), column("display_kind"),
	}, ", ") + ", " + hermesExpr(h.messages, "timestamp", "0") +
		", " + hermesExpr(h.messages, "active", "1") +
		", " + hermesExpr(h.messages, "id", "rowid") +
		" FROM messages WHERE session_id = ?"
	if h.messages["active"] {
		query += " AND (COALESCE(active, 1) != 0"
		if h.messages["compacted"] {
			query += " OR COALESCE(compacted, 0) != 0"
		}
		query += ")"
	}
	if h.messages["display_metadata"] {
		// Native display projections omit model-only scaffolding. Old schemas
		// have no metadata column, and malformed legacy JSON remains visible.
		query += " AND COALESCE(json_extract(CASE WHEN json_valid(display_metadata) THEN display_metadata ELSE '{}' END, '$.model_only'), 0) = 0"
	}
	query += " ORDER BY " + hermesExpr(h.messages, "id", "rowid")
	rows, err := q.Query(query, sid)
	if err != nil {
		return err
	}
	defer rows.Close()
	type representative struct {
		message hermesMessage
		active  int
	}
	seen := map[identity]int{}
	var messages []representative
	for rows.Next() {
		var key identity
		var m hermesMessage
		var displayKind sql.NullString
		var active int
		if err := rows.Scan(&key.role, &key.content, &key.tools, &key.toolID, &key.toolName, &displayKind, &key.at, &active, &m.id); err != nil {
			return err
		}
		m.role, m.content, m.tools, m.at = key.role.String, key.content.String, key.tools.String, key.at
		if m.role == "user" && key.content.Valid {
			key.content.String = hermesDisplayContent(m.content, displayKind.String)
		}
		if i, ok := seen[key]; ok {
			current := messages[i]
			if active > current.active || active == current.active && m.id > current.message.id {
				messages[i] = representative{m, active}
			}
		} else {
			seen[key] = len(messages)
			messages = append(messages, representative{m, active})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// A copied tail retains its first row's place, even if its selected live
	// representative was inserted after later turns in another generation.
	for _, r := range messages {
		fn(r.message)
	}
	return nil
}

func projectionDB(t *testing.T, schema string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertProjectionLegacy(t *testing.T, db *sql.DB) []hermesMessage {
	t.Helper()
	h := newHermesDB(db)
	var want, got []hermesMessage
	if err := legacyHermesProjection(h, db, "s", func(m hermesMessage) { want = append(want, m) }); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := h.eachMessage(tx, "s", func(m hermesMessage) { got = append(got, m) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection = %#v; legacy = %#v", got, want)
	}
	return got
}

func TestHermesProjectionLegacyEquivalence(t *testing.T) {
	db := projectionDB(t, `CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT COLLATE NOCASE, content TEXT COLLATE NOCASE, tool_calls TEXT, tool_call_id TEXT, tool_name TEXT, timestamp REAL, active INTEGER, compacted INTEGER, display_kind TEXT, display_metadata TEXT);`)
	carrier := hermesLegacySummary + " summary\n" + hermesHandoffEnd + "\ncurrent ask"
	insert := func(id int, role string, content, tools, toolID, toolName, at, active, compacted, kind, metadata any) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO messages VALUES (?, 's', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, role, content, tools, toolID, toolName, at, active, compacted, kind, metadata)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert(1, "user", "current ask", nil, nil, nil, 100, 0, 1, nil, nil)
	insert(2, "assistant", "between", "[]", nil, nil, 200, 1, 0, nil, nil)
	insert(3, "user", carrier, nil, nil, nil, 100, 1, 0, "hidden", nil)
	insert(4, "user", "current ask", nil, nil, nil, 100, 2, 0, nil, nil)
	insert(5, "user", carrier, nil, nil, nil, 100, 1, 0, "hidden", nil)
	insert(6, "user", "current ask", nil, nil, nil, 100, 1, 0, nil, nil)
	insert(7, "user", carrier, nil, nil, nil, 100, 0, 1, "hidden", nil)
	insert(8, "tool", "result", nil, nil, nil, 300, nil, 0, nil, nil)
	insert(9, "tool", "result", "", nil, nil, 300, 1, 0, nil, nil)
	insert(10, "tool", "result", nil, "", nil, 300, 1, 0, nil, nil)
	insert(11, "tool", "result", nil, nil, "", 300, 1, 0, nil, nil)
	insert(12, "tool", "result", nil, "call", "read", 300, 1, 0, nil, nil)
	insert(13, "tool", "result", nil, "call", "write", 300, 1, 0, nil, nil)
	insert(14, "assistant", nil, nil, nil, nil, nil, 1, 0, nil, nil)
	insert(15, "assistant", "", nil, nil, nil, 0, 1, 0, nil, nil)
	insert(16, "assistant", "Case", nil, nil, nil, 400, 1, 0, nil, nil)
	insert(17, "assistant", "case", nil, nil, nil, 400, 1, 0, nil, nil)
	insert(18, "Assistant", "case", nil, nil, nil, 400, 1, 0, nil, nil)
	insert(19, "user", "discard", nil, nil, nil, 500, 0, 0, nil, nil)
	insert(20, "user", "model", nil, nil, nil, 500, 1, 0, nil, `{"model_only":true}`)
	insert(21, "user", "malformed visible", nil, nil, nil, 500, 1, 0, nil, `{invalid`)
	insert(22, "user", carrier, nil, nil, nil, 100, 1, 0, "synthetic", nil)
	insert(23, "user", carrier, nil, nil, nil, 100, 1, 0, "hidden", nil)
	insert(24, "assistant", "between", "[]", nil, nil, 200, nil, 0, nil, nil)
	insert(25, "assistant", "between", "[]", nil, nil, 200, 0, 1, nil, nil)
	got := assertProjectionLegacy(t, db)
	var ids []int64
	for _, m := range got {
		ids = append(ids, m.id)
	}
	want := []int64{4, 24, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 21, 22}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("representatives/order = %v, want %v", ids, want)
	}
}

func TestHermesProjectionEncodedHandoffEquivalence(t *testing.T) {
	db := projectionDB(t, `CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp REAL, active INTEGER, compacted INTEGER);`)
	plain := "\x00json:" + `[{"type": "text", "text": "\u5f53\u524d\u95ee\u9898"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,AA=="}}]`
	carrier := "\x00json:" + `[{"type": "text", "text": "[PRIOR CONTEXT \u2014 for reference only; not a new message]\n\u5f53\u524d\u95ee\u9898"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,AA=="}}, {"type": "text", "text": "\n[END OF PRIOR CONTEXT \u2014 COMPACTION SUMMARY BELOW]\n[CONTEXT SUMMARY]: summary\n--- END OF CONTEXT SUMMARY \u2014 respond to the message below, not the summary above ---"}]`
	_, err := db.Exec(`INSERT INTO messages VALUES
	 (1, 's', 'user', ?, 100, 0, 1),
	 (2, 's', 'assistant', 'later turn', 200, 1, 0),
	 (3, 's', 'user', ?, 100, 1, 0),
	 (4, 's', 'user', ?, 100, 1, 0),
	 (5, 's', 'user', ?, 100, 1, 0)`, plain, carrier, plain, carrier)
	if err != nil {
		t.Fatal(err)
	}
	got := assertProjectionLegacy(t, db)
	if len(got) != 2 || got[0].id != 5 || got[0].content != carrier || got[1].id != 2 {
		t.Fatalf("encoded overlapping raw groups = %#v", got)
	}
	var absent []hermesMessage
	if err := newHermesDB(db).eachMessage(db, "absent", func(m hermesMessage) { absent = append(absent, m) }); err != nil || len(absent) != 0 {
		t.Fatalf("missing session = %#v, err %v", absent, err)
	}
}

func TestHermesProjectionLegacyOldSchemas(t *testing.T) {
	for _, schema := range []string{
		`CREATE TABLE messages (session_id TEXT, role TEXT, content TEXT, tool_calls TEXT, timestamp REAL); INSERT INTO messages VALUES ('s','user','first',NULL,NULL),('s','assistant','middle','[]',3),('s','user','first',NULL,0);`,
		`CREATE TABLE messages (id INTEGER, session_id TEXT, role TEXT, content TEXT); INSERT INTO messages VALUES (NULL,'s','user','first'),(2,'s','user','second'),(3,'s','user','first');`,
		`CREATE TABLE messages (session_id TEXT, role TEXT); INSERT INTO messages VALUES ('s','user'),('s','assistant'),('s','user');`,
		`CREATE TABLE messages (session_id TEXT, content TEXT);`,
	} {
		t.Run(schema, func(t *testing.T) { assertProjectionLegacy(t, projectionDB(t, schema)) })
	}
}

type projectionQuery struct {
	*sql.DB
	rows int
}

func (q *projectionQuery) Query(query string, args ...any) (*sql.Rows, error) {
	if err := q.DB.QueryRow("SELECT COUNT(*) FROM ("+query+")", args...).Scan(&q.rows); err != nil {
		return nil, err
	}
	return q.DB.Query(query, args...)
}

func TestHermesProjectionCollapsesBeforeScan(t *testing.T) {
	db := projectionDB(t, `CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp REAL, active INTEGER, compacted INTEGER);`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 1000; id++ {
		if _, err := tx.Exec(`INSERT INTO messages VALUES (?, 's', 'assistant', ?, 100, 1, 0)`, id, strings.Repeat("copied tail ", 100)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	h := newHermesDB(db)
	q := &projectionQuery{DB: db}
	var got []hermesMessage
	if err := h.eachMessage(q, "s", func(m hermesMessage) { got = append(got, m) }); err != nil {
		t.Fatal(err)
	}
	if q.rows != 1 {
		t.Fatalf("SQL transferred %d rows for one exact raw identity, want 1", q.rows)
	}
	if len(got) != 1 || got[0].id != 1000 {
		t.Fatalf("newest representative = %#v", got)
	}
	assertProjectionLegacy(t, db)
}
