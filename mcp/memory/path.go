package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
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

// ValidID rejects anything that is not a plain filename segment. Ids are
// attacker-influenced: they arrive from filenames on disk, from YAML
// frontmatter inside files a human can edit by hand, and eventually from
// HTTP request paths, and every one of those routes ends up interpolated
// into a filename by RepoPath or by the staging package. Anything containing
// a path separator, a ".." component, a NUL byte, a Windows drive letter, or
// a leading/trailing dot or space must fail loudly rather than resolve
// somewhere nobody asked for.
//
// This is the ONE id validator in the codebase; staging delegates to it.
func ValidID(id string) error {
	if id == "" {
		return fmt.Errorf("memory: empty id")
	}
	if strings.ContainsRune(id, 0) {
		return fmt.Errorf("memory: id %q contains a NUL byte", id)
	}
	if strings.ContainsAny(id, `/\:`) {
		return fmt.Errorf("memory: id %q contains a path separator or drive letter", id)
	}
	if id == "." || id == ".." || strings.Contains(id, "..") {
		return fmt.Errorf("memory: id %q contains a parent-directory component", id)
	}
	// A leading dot hides the file; a leading or trailing space is silently
	// stripped by Windows when the path is resolved, so two distinct ids
	// would name one file. A trailing dot is stripped the same way.
	if strings.HasPrefix(id, ".") || strings.HasSuffix(id, ".") ||
		strings.HasPrefix(id, " ") || strings.HasSuffix(id, " ") {
		return fmt.Errorf("memory: id %q has a leading or trailing dot or space", id)
	}
	if id != filepath.Base(id) {
		return fmt.Errorf("memory: id %q is not a plain filename segment", id)
	}
	return nil
}

// RepoPath returns the repo-relative, forward-slash path for a memory.
// Content addressing means two writers on different machines never target the
// same path for different content, so concurrent additions merge cleanly.
func RepoPath(m Memory) (string, error) {
	// The ID is interpolated straight into the filename below, so it is
	// validated here rather than trusted, exactly as Author is. Every caller
	// that reads an ID out of file content is one that can be handed a
	// traversing one.
	if err := ValidID(m.ID); err != nil {
		return "", fmt.Errorf("memory: cannot build path: %w", err)
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
