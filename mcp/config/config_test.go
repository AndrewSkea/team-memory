package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/config"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Token:      "ghp_test",
		Owner:      "alice",
		Repo:       "kb",
		CheckFirst: true,
	}
	path := filepath.Join(dir, "config.json")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := config.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("got %+v, want %+v", got, cfg)
	}
}

func TestReadMissing(t *testing.T) {
	_, err := config.Read("/nonexistent/path/config.json")
	if !config.IsMissing(err) {
		t.Fatalf("expected missing error, got %v", err)
	}
}

func TestWriteCreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	if err := config.Write(path, config.Config{Token: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestConfigPath(t *testing.T) {
	p := config.DefaultPath()
	if p == "" {
		t.Fatal("empty path")
	}
	if filepath.Base(p) != "config.json" {
		t.Fatalf("unexpected filename: %s", p)
	}
}
