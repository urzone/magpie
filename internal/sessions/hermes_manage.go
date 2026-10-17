package sessions

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// Keep the transaction as the query boundary so tests can reject body reads
// while observing the SQL actually executed by the management reader.
var hermesManagementQuery = func(tx *sql.Tx) hermesQuery { return tx }

// hermesManagedChecked is called under the session read locks, with closeDBs
// deferred by its caller, just like the existing full management reader.
func hermesManagedChecked() ([]Managed, error) {
	files, err := hermesFilesChecked()
	if err != nil {
		return nil, err
	}
	groups := map[string][]file{}
	for _, f := range files {
		groups[f.key] = append(groups[f.key], f)
	}
	return hermesManaged(files, groups)
}

// hermesManaged reads canonical usage aggregates for the management list.
// It does not populate the transcript/activity cache with partial states.
func hermesManaged(files []file, groups map[string][]file) ([]Managed, error) {
	states := map[string]*state{}
	for _, f := range files {
		if f.hermes == nil {
			return nil, fmt.Errorf("Hermes session store unavailable")
		}
		tx, err := f.hermes.db.BeginTx(context.Background(), nil)
		if err != nil {
			return nil, fmt.Errorf("read Hermes snapshot: %w", err)
		}
		q := hermesManagementQuery(tx)
		st := &state{Size: f.size, Mod: f.mod.UnixNano()}
		fallback, err := f.hermes.metadata(q, f.sid, st)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("read Hermes metadata: %w", err)
		}
		uses, err := f.hermes.usages(q, f.sid, fallback)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("read Hermes usage: %w", err)
		}
		for _, u := range uses {
			at := hermesTime(u.last)
			if at.IsZero() {
				at = st.Last
			}
			st.use(dateOf(at), u.model, u.Tokens)
			if at.After(st.Last) {
				st.Last = at
			}
		}
		// Old unnamed sessions retain the exact existing fallback-title rules.
		if st.Title == "" {
			err = f.hermes.eachMessage(q, f.sid, func(m hermesMessage) {
				if st.Title == "" && m.role == "user" {
					st.Title = title(hermesText(m.content))
				}
			})
			if err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("read Hermes fallback title: %w", err)
			}
		}
		tx.Rollback()
		if f.mod.After(st.Last) {
			st.Last = f.mod
		}
		states[f.path] = st
	}
	price := pricer()
	out := []Managed{}
	for _, fs := range groups {
		s, _ := assembleStates(fs, price, states)
		listed(&s)
		out = append(out, Managed{Session: s, Files: len(fs), DetailsPending: true})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Last.Equal(out[j].Last) {
			return out[i].Last.After(out[j].Last)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
