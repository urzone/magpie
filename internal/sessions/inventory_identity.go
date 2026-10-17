package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// inventoryIdentity follows assembleStates: main file, earliest basename,
// and only that main file supplies the native ID. Subagents stay grouped.
func inventoryIdentity(fs []file) string {
	id, _ := inventoryIdentityChecked(fs)
	return id
}

func inventoryIdentityChecked(fs []file) (string, error) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].main != fs[j].main {
			return fs[i].main
		}
		return filepath.Base(fs[i].path) < filepath.Base(fs[j].path)
	})
	f := fs[0]
	fallback := strings.TrimPrefix(f.key, f.agent+":")
	if !f.main {
		return fallback, nil
	}
	switch f.agent {
	case "codex", "pi", "omp", "dsh", "grok", "droid":
	default:
		return fallback, nil
	}
	// A stopped WSL source must not be opened, even for metadata.
	if f.cold {
		if st := cache[f.path]; st != nil && st.ID != "" {
			return st.ID, nil
		}
		return fallback, nil
	}
	id := ""
	switch f.agent {
	case "grok":
		if f.manifest != "" {
			raw, err := os.ReadFile(f.manifest)
			if err != nil {
				return fallback, err
			}
			var h grokSummary
			if err := json.Unmarshal(raw, &h); err != nil {
				return fallback, err
			}
			id = h.Info.ID
		}
	case "dsh":
		r, err := dshOpen(f.path)
		if err != nil {
			return fallback, err
		}
		b, readErr := bufio.NewReaderSize(r, 64<<10).ReadBytes('\n')
		_ = r.Close()
		if readErr != nil && readErr != io.EOF {
			return fallback, readErr
		}
		var h dshHead
		if json.Unmarshal(bytes.TrimSpace(b), &h) == nil && h.Type == "session" {
			id = h.ID
		}
	case "droid":
		// Validate the actual read/decompression instead of bool-only helpers
		// that intentionally suppress I/O errors for legacy discovery.
		_, err := scanAt(f.path, 0, nil, func(b []byte, _, _ int64) bool {
			var h droidStart
			if json.Unmarshal(b, &h) == nil && h.Type == "session_start" {
				id = h.ID
			}
			return false
		})
		if err != nil {
			return fallback, err
		}
	case "codex", "pi", "omp":
		marker := piHeader
		typ := "session"
		if f.agent == "codex" {
			marker = cxMeta
			typ = "session_meta"
		}
		_, err := scanAt(f.path, 0, func(b []byte) bool { return bytes.Contains(b, marker) }, func(b []byte, _, _ int64) bool {
			var h struct {
				Type    string `json:"type"`
				ID      string `json:"id"`
				Payload struct {
					ID string `json:"id"`
				} `json:"payload"`
			}
			if json.Unmarshal(b, &h) == nil && h.Type == typ {
				id = h.ID
				if f.agent == "codex" {
					id = h.Payload.ID
				}
			}
			return id == ""
		})
		if err != nil {
			return fallback, err
		}
	}
	if id != "" {
		return id, nil
	}
	return fallback, nil
}
