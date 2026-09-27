// Package secrets redacts credentials from text before it is committed to
// the shared memory repo. It is deliberately conservative: a false positive
// costs a "[REDACTED]" in a note, a false negative leaks a credential to
// everyone with repo access.
package secrets

import (
	"regexp"
	"sort"
	"strings"
)

const Mask = "[REDACTED]"

type rule struct {
	name string
	re   *regexp.Regexp
	// group is the submatch to redact; 0 redacts the whole match.
	group int
}

var rules = []rule{
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|$)`), 0},
	{"aws-access-key", regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[0-9A-Z]{16}\b`), 0},
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`), 0},
	{"github-pat", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}\b`), 0},
	{"gitlab-token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}\b`), 0},
	{"slack-token", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}\b`), 0},
	{"slack-webhook", regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/_\-]+`), 0},
	{"stripe-key", regexp.MustCompile(`\b[sr]k_(?:live|test)_[A-Za-z0-9]{16,}\b`), 0},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`), 0},
	{"anthropic-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`), 0},
	{"openai-key", regexp.MustCompile(`\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_\-]{32,}`), 0},
	{"npm-token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`), 0},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), 0},
	{"url-credentials", regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s:/@]+:([^\s@/]{3,})@`), 1},
	{"assignment", regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|secret|api[_\-]?key|apikey|access[_\-]?token|auth[_\-]?token|client[_\-]?secret|private[_\-]?key|bearer)\b["']?\s*[:=]\s*["']?([^\s"',;]{8,})`), 1},
	{"bearer-header", regexp.MustCompile(`(?i)\bauthorization:\s*(?:bearer|token|basic)\s+([A-Za-z0-9._~+/=\-]{16,})`), 1},
}

// Finding describes one redaction.
type Finding struct {
	Rule string
}

// Redact replaces anything that looks like a credential with Mask.
func Redact(text string) (string, []Finding) {
	type span struct {
		start, end int
		rule       string
	}
	var spans []span
	for _, r := range rules {
		for _, m := range r.re.FindAllStringSubmatchIndex(text, -1) {
			s, e := m[0], m[1]
			if r.group > 0 {
				if len(m) <= 2*r.group+1 || m[2*r.group] < 0 {
					continue
				}
				s, e = m[2*r.group], m[2*r.group+1]
				if isPlaceholder(text[s:e]) {
					continue
				}
			}
			spans = append(spans, span{s, e, r.name})
		}
	}
	if len(spans) == 0 {
		return text, nil
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var b strings.Builder
	var findings []Finding
	pos := 0
	for _, sp := range spans {
		if sp.start < pos { // overlapping — already covered
			if sp.end > pos {
				pos = sp.end
			}
			continue
		}
		b.WriteString(text[pos:sp.start])
		b.WriteString(Mask)
		findings = append(findings, Finding{Rule: sp.rule})
		pos = sp.end
	}
	b.WriteString(text[pos:])
	return b.String(), findings
}

// isPlaceholder filters obvious non-secrets in assignment-style matches:
// env references, templates, redaction markers and example values.
func isPlaceholder(v string) bool {
	lv := strings.ToLower(v)
	if strings.HasPrefix(v, "$") || strings.HasPrefix(v, "<") || strings.HasPrefix(v, "{") ||
		strings.HasPrefix(v, "%") || strings.HasPrefix(v, "[") || strings.HasPrefix(v, "process.env") ||
		strings.HasPrefix(lv, "os.getenv") || strings.HasPrefix(lv, "env[") || strings.HasPrefix(lv, "os.environ") {
		return true
	}
	for _, p := range []string{"xxxx", "****", "changeme", "example", "placeholder", "your_", "your-", "redacted", "dummy", "<", "..."} {
		if strings.Contains(lv, p) {
			return true
		}
	}
	// Identifiers like getPassword() or config.secret are code, not values.
	if strings.ContainsAny(v, "()") {
		return true
	}
	return false
}
