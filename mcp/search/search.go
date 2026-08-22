// Package search provides full-text search over memories.
//
// The index is a fully derived, disposable artifact. Delete the database and
// call Rebuild to reconstruct it from the Markdown files. Because it is never
// a source of truth, index corruption is never data loss.
package search

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	_ "modernc.org/sqlite"
)

// Hit is one search result.
type Hit struct {
	ID      string
	Slug    string
	Scope   memory.Scope
	Snippet string
}

// Index wraps the SQLite FTS5 database.
type Index struct {
	db *sql.DB
}

const schema = `
CREATE VIRTUAL TABLE IF NOT EXISTS memories USING fts5(
    id UNINDEXED,
    slug,
    scope UNINDEXED,
    author,
    tags,
    body
);`

// Open opens or creates the index database at dbPath.
//
// dbPath must live outside the git worktree (paths.Layout.Index, not
// paths.Layout.Clone): the index is a local, derived artifact and must never
// be staged or committed into the shared repository.
func Open(dbPath string) (*Index, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("search: open %q: %w", dbPath, err)
	}
	// FTS5 does not tolerate concurrent writers across connections; a single
	// connection avoids SQLITE_BUSY without needing a busy-timeout dance.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("search: create schema: %w", err)
	}
	return &Index{db: db}, nil
}

// Close releases the database handle.
func (ix *Index) Close() error { return ix.db.Close() }

// Rebuild replaces the entire index contents. Idempotent by construction: the
// table is emptied first, so repeated rebuilds never duplicate rows. Callers
// should pass the output of store.All() — the memory files are the only
// source of truth, and the index must always be reconstructible from them.
func (ix *Index) Rebuild(ms []memory.Memory) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return fmt.Errorf("search: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM memories`); err != nil {
		return fmt.Errorf("search: clear index: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO memories (id, slug, scope, author, tags, body) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("search: prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, m := range ms {
		_, err := stmt.Exec(
			m.ID,
			strings.ReplaceAll(m.Slug, "-", " "),
			string(m.Scope),
			m.Author,
			strings.Join(m.Tags, " "),
			m.Body,
		)
		if err != nil {
			return fmt.Errorf("search: index memory %s: %w", m.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: commit index: %w", err)
	}
	return nil
}

// Search runs an FTS5 MATCH query and returns up to limit hits ordered by
// relevance.
//
// The query is never interpolated into SQL — it is always passed as a bound
// parameter. It is also always wrapped as a single FTS5 quoted phrase (with
// any embedded double quotes doubled per FTS5's escaping rule), so raw FTS5
// operator syntax in user input — bare *, AND/OR/NOT/NEAR, unbalanced quotes,
// column filters — can never change the query's structure or trigger an FTS5
// syntax error. An empty (or whitespace-only) query deliberately matches
// nothing rather than being treated as "match everything".
func (ix *Index) Search(q string, limit int) ([]Hit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`

	rows, err := ix.db.Query(`
        SELECT id, slug, scope, snippet(memories, 5, '', '', '…', 20)
        FROM memories
        WHERE memories MATCH ?
        ORDER BY rank
        LIMIT ?`, phrase, limit)
	if err != nil {
		return nil, fmt.Errorf("search: query %q: %w", q, err)
	}
	defer rows.Close()

	var out []Hit
	for rows.Next() {
		var h Hit
		var scope string
		if err := rows.Scan(&h.ID, &h.Slug, &scope, &h.Snippet); err != nil {
			return nil, fmt.Errorf("search: scan row: %w", err)
		}
		h.Scope = memory.Scope(scope)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: iterate rows: %w", err)
	}
	return out, nil
}
