package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func DefaultPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "team-memory", "config.json")
}

func Read(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, &missingError{path: path}
	}
	if err != nil {
		return Config{}, err
	}
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
