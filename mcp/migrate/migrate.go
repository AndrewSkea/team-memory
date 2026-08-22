package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndrewSkea/team-memory/mcp/staging"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

// Result counts what a migration run did.
type Result struct {
	Converted int // committed as v2 memories
	Unparsed  int // preserved verbatim under memories/team/unparsed/
	Pending   int // UNSURE.md entries routed to the review queue
}

// Run converts every v1 Markdown document in the clone.
//
// UNSURE.md entries go to the review queue rather than being promoted, because
// v1 could not decide where they belonged and neither should the migration.
//
// Run never drops an entry: every block it reads ends up converted, staged
// for review, or preserved verbatim under memories/team/unparsed/.
func Run(s *store.Store, a *staging.Area) (Result, error) {
	var res Result
	clone := s.Layout().Clone

	docs, err := v1Documents(clone)
	if err != nil {
		return res, err
	}

	for _, doc := range docs {
		content, err := os.ReadFile(filepath.Join(clone, doc))
		if err != nil {
			return res, fmt.Errorf("migrate: read %q: %w", doc, err)
		}
		parsed, bad := ParseEntries(doc, string(content))

		unsure := strings.EqualFold(filepath.Base(doc), "UNSURE.md")
		for _, m := range parsed {
			if unsure {
				if err := a.Add(m); err != nil {
					return res, err
				}
				res.Pending++
				continue
			}
			if _, err := s.Put(m, "team-memory: migrate "+m.ID+" from "+doc); err != nil {
				return res, err
			}
			res.Converted++
		}

		for _, u := range bad {
			if err := preserve(clone, s, u); err != nil {
				return res, err
			}
			res.Unparsed++
		}
	}

	if res.Converted > 0 || res.Unparsed > 0 {
		if _, err := s.RegenerateIndex(); err != nil {
			return res, err
		}
	}
	return res, nil
}

// v1Documents lists Markdown files at the clone root and in legacy topic
// directories, excluding anything already under memories/ and excluding the
// derived INDEX.md.
func v1Documents(clone string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(clone, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(clone, path)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(rel)

		if d.IsDir() {
			if slashed == ".git" || slashed == "memories" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || slashed == "INDEX.md" {
			return nil
		}
		out = append(out, slashed)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("migrate: scan clone: %w", err)
	}
	return out, nil
}

// preserve writes an unparsable block verbatim so nothing is ever lost. The
// filename is a hash of the raw block, which makes re-running the migration
// idempotent: an identical unparsable block on a second run resolves to the
// same path and PutRaw's commit collapses to a no-op rather than a
// duplicate.
func preserve(clone string, s *store.Store, u Unparsed) error {
	sum := sha256.Sum256([]byte(u.Raw))
	name := hex.EncodeToString(sum[:])[:16] + ".md"
	rel := filepath.ToSlash(filepath.Join("memories", "team", "unparsed", name))
	abs := filepath.Join(clone, filepath.FromSlash(rel))

	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return fmt.Errorf("migrate: create unparsed directory: %w", err)
	}
	body := fmt.Sprintf("<!-- source: %s -->\n<!-- reason: %s -->\n\n%s\n", u.Source, u.Reason, u.Raw)
	if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
		return fmt.Errorf("migrate: write unparsed %q: %w", rel, err)
	}
	if _, err := s.PutRaw(rel, "team-memory: preserve unparsed entry from "+u.Source); err != nil {
		return err
	}
	return nil
}
