package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Config struct {
	Token string `json:"token,omitempty"`
	// TokenSource "gh" means: ask the GitHub CLI for a token at runtime
	// (`gh auth token`) instead of storing one in this file.
	TokenSource string `json:"token_source,omitempty"`
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	// Mode is this user's capture mode (auto, ai, review, review-timeout,
	// manual). Empty means "use the team default".
	Mode string `json:"mode,omitempty"`
	// Username is the GitHub login of this user, recorded as memory author.
	Username string `json:"username,omitempty"`
	// APIURL overrides the GitHub API base (GitHub Enterprise Server).
	APIURL     string `json:"api_url,omitempty"`
	CheckFirst bool   `json:"check_first,omitempty"`
}

// Configured reports whether enough is set to talk to the memory repo.
func (c Config) Configured() bool {
	return c.Owner != "" && c.Repo != "" && (c.Token != "" || c.TokenSource == "gh")
}

// Slug returns "owner/repo".
func (c Config) Slug() string { return c.Owner + "/" + c.Repo }

// APIBase returns the GitHub REST API base URL.
func (c Config) APIBase() string {
	if v := os.Getenv("TEAM_MEMORY_API_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if c.APIURL != "" {
		return strings.TrimRight(c.APIURL, "/")
	}
	return "https://api.github.com"
}

// ResolveToken returns the GitHub token, consulting the GitHub CLI when the
// config delegates to it.
func (c Config) ResolveToken() (string, error) {
	if c.Token != "" {
		return c.Token, nil
	}
	if c.TokenSource == "gh" {
		return GHToken()
	}
	return "", errors.New("no GitHub token configured")
}

// GHToken asks the GitHub CLI for the logged-in user's token.
func GHToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token: %w (is the GitHub CLI installed and logged in?)", err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", errors.New("gh auth token returned nothing — run `gh auth login`")
	}
	return tok, nil
}

type missingError struct{ path string }

func (e *missingError) Error() string { return "team-memory: no config found at " + e.path }

func IsMissing(err error) bool {
	var m *missingError
	return errors.As(err, &m)
}

// DefaultPath returns the CLI config location. It must match where
// install.sh / install.ps1 write the file:
//   - Windows: %APPDATA%\team-memory\config.json (os.UserConfigDir)
//   - macOS/Linux: $XDG_CONFIG_HOME/team-memory/config.json, else ~/.config/…
//
// os.UserConfigDir alone is wrong on macOS (~/Library/Application Support),
// which is where older builds looked; that location is still honoured as a
// fallback if it exists and the ~/.config file does not.
//
// TEAM_MEMORY_CONFIG overrides the path entirely.
func DefaultPath() string {
	if v := os.Getenv("TEAM_MEMORY_CONFIG"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		dir, _ := os.UserConfigDir()
		return filepath.Join(dir, "team-memory", "config.json")
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	path := filepath.Join(dir, "team-memory", "config.json")
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if legacy, _ := os.UserConfigDir(); legacy != "" {
				lp := filepath.Join(legacy, "team-memory", "config.json")
				if _, err := os.Stat(lp); err == nil {
					return lp
				}
			}
		}
	}
	return path
}

// DataDir holds the local mirror, pending review queue, capture queue and
// logs. TEAM_MEMORY_DATA overrides it.
//   - Windows: %LOCALAPPDATA%\team-memory
//   - macOS/Linux: $XDG_DATA_HOME/team-memory, else ~/.local/share/team-memory
func DataDir() string {
	if v := os.Getenv("TEAM_MEMORY_DATA"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "team-memory")
		}
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "team-memory")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "team-memory")
}

func Read(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, &missingError{path: path}
	}
	if err != nil {
		return Config{}, err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // strip UTF-8 BOM (Windows PowerShell writes it)
	var cfg Config
	return cfg, json.Unmarshal(data, &cfg)
}

func Write(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
