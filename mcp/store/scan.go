package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/go-git/go-git/v5/plumbing"
)

const memoriesDir = "memories"

// All loads every parsable memory from the clone, newest first. A file that
// fails to parse is skipped rather than failing the whole scan — one corrupt
// file must not make the entire memory store unreadable.
func (s *Store) All() ([]memory.Memory, error) {
	root := filepath.Join(s.layout.Clone, memoriesDir)
	var out []memory.Memory

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // no memories yet
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		m, parseErr := memory.Unmarshal(data)
		if parseErr != nil {
			return nil
		}
		// The ID comes from file content, and All is the one reader that
		// cannot re-derive it from the filename (the filename is
		// "<id>-<slug>.md", and a slug may contain hyphens, so the split is
		// ambiguous). Refuse an ID that could not have produced a path under
		// memories/ rather than hand it to a caller that will feed it back
		// into RepoPath.
		if memory.ValidID(m.ID) != nil {
			return nil
		}
		out = append(out, m)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: scan memories: %w", err)
	}

	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// RenderIndex produces INDEX.md from the memories themselves. It is a pure
// function: no filesystem access, so regenerating from the same slice always
// yields the same bytes and is cheap to test for determinism.
//
// Every field that ultimately comes from a memory file's frontmatter (ID,
// scope, author, tags, slug) is attacker-or-accident controlled text —
// memory.Unmarshal reads it straight off disk with no validation upstream —
// so each is escaped at this render boundary before it goes into a table
// cell. Escaping here, not upstream, is deliberate: this is the one place
// that knows it is building a Markdown table and has one to corrupt.
func RenderIndex(ms []memory.Memory) string {
	var b strings.Builder
	b.WriteString("# INDEX for team-memory\n\n")
	b.WriteString("Generated file. Do not edit by hand.\n\n")
	b.WriteString("| id | created | scope | author | tags | title |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, m := range ms {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			escapeCell(m.ID),
			m.Created.UTC().Format("2006-01-02"),
			escapeCell(string(m.Scope)),
			escapeCell(m.Author),
			escapeCell(strings.Join(m.Tags, " ")),
			escapeCell(m.Slug),
		)
	}
	return b.String()
}

// escapeCell makes s safe to interpolate into a single Markdown table cell:
// afterward it can contain neither "|" (which would open a spurious cell)
// nor any newline form (which would split the row and derange every row
// that follows it in the file). Backslash is escaped first, before any
// escape sequence is introduced, so the escaping itself stays unambiguous
// and reversible.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\r\n", `\n`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\n`)
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}

// RegenerateIndex rewrites INDEX.md from the memories on disk and commits it
// through the single commitPath funnel — no second worktree.Add site.
func (s *Store) RegenerateIndex() (plumbing.Hash, error) {
	ms, err := s.All()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	abs := filepath.Join(s.layout.Clone, "INDEX.md")
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := os.WriteFile(abs, []byte(RenderIndex(ms)), 0o600); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: write INDEX.md: %w", err)
	}
	return s.commitPath(abs, "team-memory: regenerate INDEX.md")
}
