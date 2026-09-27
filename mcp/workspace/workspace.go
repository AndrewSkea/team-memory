// Package workspace inspects the directory a Claude Code session runs in:
// which GitHub repo it belongs to (read straight from .git/config, so no git
// binary is required) and which systems it touches (dependency manifests and
// environment variable *names* — values are never read).
package workspace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Repo identifies the git repository containing a directory.
type Repo struct {
	Root  string
	Host  string
	Owner string
	Name  string
}

// Slug returns owner/name, or "" if unknown.
func (r Repo) Slug() string {
	if r.Owner == "" || r.Name == "" {
		return ""
	}
	return r.Owner + "/" + r.Name
}

// FindRepo walks up from dir to the enclosing git repository and reads its
// origin remote (falling back to the first remote).
func FindRepo(dir string) (Repo, bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Repo{}, false
	}
	for {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			gitDir := gitPath
			if !info.IsDir() {
				gitDir = resolveGitFile(gitPath, dir)
				if gitDir == "" {
					return Repo{Root: dir}, true
				}
			}
			r := Repo{Root: dir}
			if u := remoteURL(filepath.Join(commonDir(gitDir), "config")); u != "" {
				r.Host, r.Owner, r.Name = ParseRemote(u)
			}
			return r, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Repo{}, false
		}
		dir = parent
	}
}

// resolveGitFile handles worktrees/submodules where .git is a file
// containing "gitdir: <path>".
func resolveGitFile(gitFile, base string) string {
	b, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, "gitdir:") {
		return ""
	}
	p := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return p
}

// commonDir follows a worktree's "commondir" pointer to the main .git dir,
// which is where config lives.
func commonDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	p := strings.TrimSpace(string(b))
	if !filepath.IsAbs(p) {
		p = filepath.Join(gitDir, p)
	}
	return filepath.Clean(p)
}

var sectionRe = regexp.MustCompile(`^\[\s*remote\s+"([^"]+)"\s*\]$`)

func remoteURL(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	urls := map[string]string{}
	var order []string
	cur := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			cur = ""
			if m := sectionRe.FindStringSubmatch(line); m != nil {
				cur = m[1]
			}
			continue
		}
		if cur == "" {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "url" {
			if _, seen := urls[cur]; !seen {
				order = append(order, cur)
			}
			urls[cur] = strings.TrimSpace(v)
		}
	}
	if u, ok := urls["origin"]; ok {
		return u
	}
	if len(order) > 0 {
		return urls[order[0]]
	}
	return ""
}

// ParseRemote extracts host/owner/name from common git remote URL forms:
// git@host:owner/name.git, https://host/owner/name(.git), ssh://git@host/owner/name.
func ParseRemote(u string) (host, owner, name string) {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	var p string
	switch {
	case strings.Contains(u, "://"):
		rest := u[strings.Index(u, "://")+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 && at < strings.Index(rest+"/", "/") {
			rest = rest[at+1:]
		}
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return "", "", ""
		}
		host, p = rest[:slash], rest[slash+1:]
		if i := strings.Index(host, ":"); i >= 0 {
			host = host[:i]
		}
	case strings.Contains(u, ":"):
		i := strings.Index(u, ":")
		host, p = u[:i], u[i+1:]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
	default:
		return "", "", ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 {
		return host, "", ""
	}
	// GitLab subgroups: owner is everything but the last segment.
	return host, strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
}

// Signals returns names describing what a workspace touches: the repo name,
// declared dependencies and env var names. They are matched against the
// entity registry to find relevant memories.
func Signals(dir string) []string {
	root := dir
	if r, ok := FindRepo(dir); ok && r.Root != "" {
		root = r.Root
	}
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) >= 2 && len(s) <= 120 && !seen[s] {
			seen[s] = true
		}
	}
	add(filepath.Base(root))
	if r, ok := FindRepo(dir); ok && r.Name != "" {
		add(r.Name)
	}
	for _, d := range uniqDirs(root, dir) {
		packageJSON(filepath.Join(d, "package.json"), add)
		goMod(filepath.Join(d, "go.mod"), add)
		lineList(filepath.Join(d, "requirements.txt"), reqRe, add)
		quotedDeps(filepath.Join(d, "pyproject.toml"), add)
		tomlDeps(filepath.Join(d, "Cargo.toml"), add)
		for _, env := range []string{".env", ".env.example", ".env.sample", ".env.local", ".env.development"} {
			lineList(filepath.Join(d, env), envRe, add)
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) > 400 {
		out = out[:400]
	}
	return out
}

func uniqDirs(a, b string) []string {
	if a == b {
		return []string{a}
	}
	return []string{a, b}
}

func readSmall(p string) []byte {
	info, err := os.Stat(p)
	if err != nil || info.Size() > 1<<20 {
		return nil
	}
	b, _ := os.ReadFile(p)
	return b
}

func packageJSON(p string, add func(string)) {
	b := readSmall(p)
	if b == nil {
		return
	}
	var pj struct {
		Name            string            `json:"name"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(b, &pj) != nil {
		return
	}
	add(pj.Name)
	for k := range pj.Dependencies {
		add(k)
	}
	for k := range pj.DevDependencies {
		add(k)
	}
}

func goMod(p string, add func(string)) {
	b := readSmall(p)
	if b == nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) >= 2 && f[0] == "module" {
			add(f[1])
			add(filepath.Base(f[1]))
			continue
		}
		if len(f) >= 2 && strings.Contains(f[0], ".") && strings.HasPrefix(f[1], "v") {
			add(f[0])
			add(filepath.Base(f[0]))
		}
	}
}

var (
	reqRe    = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._\-]*)`)
	envRe    = regexp.MustCompile(`^\s*(?:export\s+)?([A-Z][A-Z0-9_]{2,})\s*=`)
	quotedRe = regexp.MustCompile(`"([A-Za-z0-9][A-Za-z0-9._\-]*)\s*(?:[<>=!~\[;].*)?"`)
	tomlKey  = regexp.MustCompile(`^\s*([A-Za-z0-9_\-]+)\s*=`)
)

func lineList(p string, re *regexp.Regexp, add func(string)) {
	b := readSmall(p)
	if b == nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := re.FindStringSubmatch(line); m != nil {
			add(m[1])
		}
	}
}

func quotedDeps(p string, add func(string)) {
	b := readSmall(p)
	if b == nil {
		return
	}
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "dependencies") || strings.HasPrefix(t, "dev-dependencies") {
			in = true
		}
		if in {
			for _, m := range quotedRe.FindAllStringSubmatch(t, -1) {
				add(m[1])
			}
			if strings.Contains(t, "]") {
				in = false
			}
		}
	}
}

func tomlDeps(p string, add func(string)) {
	b := readSmall(p)
	if b == nil {
		return
	}
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			in = strings.Contains(t, "dependencies")
			continue
		}
		if in {
			if m := tomlKey.FindStringSubmatch(t); m != nil {
				add(m[1])
			}
		}
	}
}
