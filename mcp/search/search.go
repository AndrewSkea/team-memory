// Package search ranks memories for a query. It is an in-memory BM25 index
// with boosts for entity ("about") overlap, recency and provenance. Team
// memory repos are small (thousands of entries), so rebuilding the index on
// every process start takes milliseconds and needs no persistent state.
package search

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/AndrewSkea/team-memory/mcp/entities"
	"github.com/AndrewSkea/team-memory/mcp/memory"
)

const (
	k1 = 1.2
	b  = 0.75
)

type doc struct {
	m      memory.Memory
	tf     map[string]int
	length int
	about  map[string]bool // normalised entity names
}

type Index struct {
	docs  []doc
	df    map[string]int
	avgDL float64
}

// Build indexes memories. Field weights are applied by term repetition.
func Build(ms []memory.Memory) *Index {
	idx := &Index{df: map[string]int{}}
	total := 0
	for _, m := range ms {
		d := doc{m: m, tf: map[string]int{}, about: map[string]bool{}}
		add := func(text string, weight int) {
			for _, t := range Tokenize(text) {
				d.tf[t] += weight
				d.length += weight
			}
		}
		add(m.Title, 3)
		add(strings.Join(m.About, " "), 3)
		add(strings.Join(m.Tags, " "), 2)
		add(m.Summary, 2)
		add(m.Body, 1)
		for _, a := range m.About {
			d.about[entities.CanonicalName(a)] = true
		}
		for t := range d.tf {
			idx.df[t]++
		}
		total += d.length
		idx.docs = append(idx.docs, d)
	}
	if len(idx.docs) > 0 {
		idx.avgDL = float64(total) / float64(len(idx.docs))
	}
	return idx
}

// Len returns the number of indexed memories.
func (idx *Index) Len() int { return len(idx.docs) }

// Query describes a search.
type Query struct {
	Text string
	// Entities in play (canonical names). StrongEntities (e.g. mentioned in
	// the prompt) count more than ambient ones (e.g. repo dependencies).
	StrongEntities []string
	WeakEntities   []string
	Type           string
	Viewer         string // GitHub login; personal memories only match their author
	Repo           string // owner/repo of the working dir — small provenance boost
	Limit          int
	IncludeAll     bool // include superseded/retracted
	Exclude        map[string]bool
	Now            time.Time
}

type Result struct {
	Memory       memory.Memory `json:"memory"`
	Score        float64       `json:"score"`
	Coverage     float64       `json:"coverage"` // fraction of query terms matched
	EntityHits   []string      `json:"entity_hits,omitempty"`
	StrongEntity bool          `json:"-"`
}

// Search ranks memories. An empty query with entities returns memories about
// those entities; an empty query with no entities returns the most recent.
func (idx *Index) Search(q Query) []Result {
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	terms := uniq(Tokenize(q.Text))
	strong := set(q.StrongEntities)
	weak := set(q.WeakEntities)
	var out []Result
	n := float64(len(idx.docs))
	for _, d := range idx.docs {
		m := d.m
		if q.Exclude[m.ID] {
			continue
		}
		if !q.IncludeAll && !m.Active() {
			continue
		}
		if m.Scope == memory.ScopePersonal && (q.Viewer == "" || !strings.EqualFold(m.Author, q.Viewer)) {
			continue
		}
		if q.Type != "" && !strings.EqualFold(m.Type, q.Type) {
			continue
		}
		var score float64
		matched := 0
		for _, t := range terms {
			tf := float64(d.tf[t])
			if tf == 0 {
				continue
			}
			matched++
			df := float64(idx.df[t])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			score += idf * tf * (k1 + 1) / (tf + k1*(1-b+b*float64(d.length)/idx.avgDL))
		}
		var hits []string
		isStrong := false
		for e := range d.about {
			if strong[e] {
				hits = append(hits, e)
				score += 4
				isStrong = true
			} else if weak[e] {
				hits = append(hits, e)
				score += 1.5
			}
		}
		if score == 0 && (len(terms) > 0 || len(strong)+len(weak) > 0) {
			continue
		}
		if q.Repo != "" && strings.EqualFold(m.LearnedIn, q.Repo) {
			score += 0.5
		}
		// Gentle recency preference: a two-year-old memory keeps ~70% weight.
		if !m.Created.IsZero() {
			age := q.Now.Sub(m.Created).Hours() / 24
			if age > 0 {
				score *= 0.7 + 0.3*math.Exp(-age/365)
			}
		}
		if m.Legacy {
			score *= 0.9
		}
		cov := 0.0
		if len(terms) > 0 {
			cov = float64(matched) / float64(len(terms))
		}
		sort.Strings(hits)
		out = append(out, Result{Memory: m, Score: score, Coverage: cov, EntityHits: hits, StrongEntity: isStrong})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Memory.Created.After(out[j].Memory.Created)
	})
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out
}

// Relevant reports whether a result is strong enough to inject into a
// conversation unprompted: it is about a system named in the prompt, or it
// matches most of a multi-word query.
func (r Result) Relevant() bool {
	if r.StrongEntity {
		return true
	}
	return r.Coverage >= 0.6 && r.Score >= 3
}

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be but by can could did do does doing done for from had has have
		how i if in into is it its just let me my no not of on or our please should so some than that the their them
		then there these they this to too up us was we were what when where which who why will with would you your
		about after again all also am any because been before being both each few further here more most much must
		now off once only other out over own same she he her him his very yeah yes ok okay hey hi thanks thank
		use using used make made get got want need like know see look looks let's im i'm it's dont don't can't
		file files code line lines function work working works thing things way new one two`) {
		stop[w] = true
	}
}

// Tokenize lowercases, splits on non-alphanumerics, drops stopwords and
// applies light plural stemming.
func Tokenize(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(f) < 2 || stop[f] {
			continue
		}
		out = append(out, stem(f))
	}
	return out
}

func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 4 && strings.HasSuffix(w, "ses"):
		return w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is"):
		return w[:len(w)-1]
	}
	return w
}

func uniq(ts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range ts {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[entities.CanonicalName(x)] = true
	}
	return m
}
