package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	Token      string `json:"token"`
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	CheckFirst bool   `json:"check_first"`
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
func DefaultPath() string {
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
	return os.WriteFile(path, data, 0o600)
}
