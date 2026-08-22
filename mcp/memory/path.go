package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	idLen      = 16
	maxSlugLen = 48
)

// ComputeID hashes only the identity-bearing fields: body, author and creation
// time. Scope, tags and slug are review-time corrections and must not move a
// memory to a different path when they change.
func ComputeID(m Memory) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s",
		m.Body, m.Author, m.Created.UTC().Format("2006-01-02T15:04:05Z"))
	return hex.EncodeToString(h.Sum(nil))[:idLen]
}

// Slugify produces a filesystem- and URL-safe fragment. Non-ASCII bytes become
// hyphens rather than being transliterated, so the result is stable across
// platforms without a Unicode table.
func Slugify(s string) string {
	var b strings.Builder
	lastHyphen := true // leading hyphens are suppressed
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > maxSlugLen {
		out = strings.Trim(out[:maxSlugLen], "-")
	}
	if out == "" {
		return "untitled"
	}
	return out
}

// RepoPath returns the repo-relative, forward-slash path for a memory.
// Content addressing means two writers on different machines never target the
// same path for different content, so concurrent additions merge cleanly.
func RepoPath(m Memory) (string, error) {
	if m.ID == "" {
		return "", fmt.Errorf("memory: cannot build path, ID is empty")
	}
	slug := Slugify(m.Slug)
	dateDir := m.Created.UTC().Format("2006/01")
	name := m.ID + "-" + slug + ".md"

	switch m.Scope {
	case ScopePersonal:
		// Reject separators before slugifying. Slugify alone would turn
		// "../../etc" into the harmless-looking "etc", silently filing the
		// memory under the wrong author instead of refusing.
		if strings.ContainsAny(m.Author, `/\`) || strings.Contains(m.Author, "..") {
			return "", fmt.Errorf("memory: author %q contains path separators", m.Author)
		}
		author := Slugify(m.Author)
		if m.Author == "" || author == "untitled" {
			return "", fmt.Errorf("memory: personal memory %s has no usable author", m.ID)
		}
		return "memories/people/" + author + "/" + dateDir + "/" + name, nil
	case ScopeTeam, "":
		return "memories/team/" + dateDir + "/" + name, nil
	default:
		return "", fmt.Errorf("memory: unknown scope %q", m.Scope)
	}
}
