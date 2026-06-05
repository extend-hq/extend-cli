// Package config reads and writes the CLI's on-disk configuration file,
// written by `extend setup` and consumed as the lowest-precedence source
// of auth/routing settings by every command (flag > env > this file >
// default).
//
// The file lives at $XDG_CONFIG_HOME/extend/config.json, falling back to
// ~/.config/extend/config.json. It holds nothing the user can't also pass
// via environment variables; it exists so an interactive `extend setup`
// can persist a working configuration once instead of asking the user to
// edit their shell profile.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// File is the on-disk configuration. Every field is optional; the zero
// value means "fall back to the next source in the precedence chain".
// Field names mirror the JSON written to disk so a human can hand-edit
// the file if they prefer.
type File struct {
	// Region is the short region selector (us|us2|eu) used to pick the
	// API base URL when neither --region nor EXTEND_REGION is set.
	Region string `json:"region,omitempty"`
	// APIKey is the bearer token (sk_...). Stored only for the default
	// environment; --env labels always resolve their key from the
	// environment, never from this file.
	APIKey string `json:"apiKey,omitempty"`
}

// Path returns the configuration file location. It honors
// XDG_CONFIG_HOME and otherwise falls back to ~/.config, matching the
// platform-agnostic convention the rest of the CLI uses for user files.
func Path() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "extend", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "extend", "config.json"), nil
}

// Load reads and parses the configuration file. A missing file is not an
// error: it returns the zero File and nil so callers can treat "no config
// yet" identically to "config with no overrides".
func Load() (File, error) {
	path, err := Path()
	if err != nil {
		return File{}, err
	}
	return loadFrom(path)
}

func loadFrom(path string) (File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return File{}, nil
		}
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, nil
}

// Save writes f to the configuration path, creating the parent directory
// (0700) if needed and writing the file with 0600 permissions since it
// contains a credential. The write is atomic: a temp file in the same
// directory is written and renamed into place. It returns the path
// written so callers can report it.
func Save(f File) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := saveTo(path, f); err != nil {
		return "", err
	}
	return path, nil
}

func saveTo(path string, f File) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install config: %w", err)
	}
	return nil
}
