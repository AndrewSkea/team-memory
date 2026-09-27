package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRemote(t *testing.T) {
	cases := map[string][3]string{
		"git@github.com:acme/billing.git":               {"github.com", "acme", "billing"},
		"https://github.com/acme/billing":               {"github.com", "acme", "billing"},
		"https://user:tok@github.com/acme/billing.git/": {"github.com", "acme", "billing"},
		"ssh://git@github.example.com:22/acme/billing":  {"github.example.com", "acme", "billing"},
		"https://gitlab.com/group/sub/project.git":      {"gitlab.com", "group/sub", "project"},
		"not a url": {"", "", ""},
	}
	for in, want := range cases {
		h, o, n := ParseRemote(in)
		if [3]string{h, o, n} != want {
			t.Errorf("%s: got %s %s %s", in, h, o, n)
		}
	}
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindRepoAndWorktree(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "main", ".git", "config"), `[core]
	bare = false
[remote "upstream"]
	url = git@github.com:other/fork.git
[remote "origin"]
	url = git@github.com:acme/checkout-web.git
`)
	sub := filepath.Join(root, "main", "src", "deep")
	os.MkdirAll(sub, 0o755)
	r, ok := FindRepo(sub)
	if !ok || r.Slug() != "acme/checkout-web" || r.Root != filepath.Join(root, "main") {
		t.Fatalf("%+v %v", r, ok)
	}
	// Worktree: .git is a file pointing at .git/worktrees/wt, which has commondir.
	wtGit := filepath.Join(root, "main", ".git", "worktrees", "wt")
	write(t, filepath.Join(wtGit, "commondir"), "../..\n")
	write(t, filepath.Join(root, "wt", ".git"), "gitdir: "+wtGit+"\n")
	r, ok = FindRepo(filepath.Join(root, "wt"))
	if !ok || r.Slug() != "acme/checkout-web" {
		t.Fatalf("worktree: %+v %v", r, ok)
	}
	if _, ok := FindRepo(t.TempDir()); ok {
		// TempDir may be inside a repo on some machines; only fail if it
		// claims our fake remote.
		if r, _ := FindRepo(t.TempDir()); r.Slug() == "acme/checkout-web" {
			t.Fatal("leaked")
		}
	}
}

func TestSignals(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/acme/checkout-web\n")
	write(t, filepath.Join(root, "package.json"), `{"name":"checkout","dependencies":{"@acme/payments-client":"1"},"devDependencies":{"jest":"1"}}`)
	write(t, filepath.Join(root, "go.mod"), "module github.com/acme/checkout\n\nrequire (\n\tgithub.com/segmentio/kafka-go v0.4.0\n)\n")
	write(t, filepath.Join(root, "requirements.txt"), "# comment\nrequests==2.0\nboto3>=1\n")
	write(t, filepath.Join(root, "pyproject.toml"), "[project]\ndependencies = [\n  \"httpx>=0.27\",\n  \"pydantic\",\n]\n")
	write(t, filepath.Join(root, "Cargo.toml"), "[dependencies]\nserde = \"1\"\n[package]\nname = \"x\"\n")
	write(t, filepath.Join(root, ".env"), "PAYMENTS_API_URL=https://secret.internal\nexport DB_PASSWORD=hunter2\nlower=1\n")
	got := strings.Join(Signals(root), ",")
	for _, want := range []string{"checkout-web", "@acme/payments-client", "jest", "kafka-go", "github.com/acme/checkout",
		"requests", "boto3", "httpx", "pydantic", "serde", "PAYMENTS_API_URL", "DB_PASSWORD"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	for _, bad := range []string{"hunter2", "secret.internal", "lower"} {
		if strings.Contains(got, bad) {
			t.Errorf("leaked %q", bad)
		}
	}
}
