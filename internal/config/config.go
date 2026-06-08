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

// Version is the current on-disk schema version. Save stamps it into
// every file it writes; Load tolerates older or absent versions and
// migrates them in memory, so a future schema change (e.g. OAuth login)
// is an additive bump rather than a breaking rewrite.
const Version = 1

// AuthMethod discriminates how the stored credential authenticates. Only
// api_key exists today; oauth is reserved so adding interactive login is
// a new variant on Auth rather than a schema break.
type AuthMethod string

const (
	// AuthAPIKey is a static bearer API key (sk_...).
	AuthAPIKey AuthMethod = "api_key"
)

// Auth is the credential block. It is a tagged union keyed by Type; the
// fields populated depend on the method. Keeping it self-contained means
// it can later move to an OS keychain without disturbing the rest of the
// file.
type Auth struct {
	// Type selects which of the fields below are meaningful.
	Type AuthMethod `json:"type"`
	// APIKey is the bearer token for Type == AuthAPIKey.
	APIKey string `json:"apiKey,omitempty"`
	// OAuth fields are reserved for a future interactive-login flow:
	// AccessToken/RefreshToken string and an expiry timestamp. They are
	// intentionally omitted until that lands so the shape stays minimal.
}

// File is the on-disk configuration. Every field is optional; the zero
// value means "fall back to the next source in the precedence chain".
// Field names mirror the JSON written to disk so a human can hand-edit
// the file if they prefer.
type File struct {
	// Version is the schema version this file was written with. Stamped
	// by Save; consulted by Load's migration path.
	Version int `json:"version,omitempty"`
	// Region is the short region selector (us|eu) used to pick the API
	// base URL when neither --region nor EXTEND_REGION nor BaseURL is set.
	Region string `json:"region,omitempty"`
	// BaseURL overrides the region-derived API URL. For self-hosted or
	// staging endpoints; EXTEND_BASE_URL takes precedence over it.
	BaseURL string `json:"baseUrl,omitempty"`
	// WorkspaceID scopes an organization-level API key to a workspace;
	// EXTEND_WORKSPACE_ID takes precedence over it.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// Auth is the credential block. A nil Auth means no stored credential.
	Auth *Auth `json:"auth,omitempty"`
}

// APIKey returns the stored API key, or "" when the file holds no
// credential or uses a non-key auth method. Callers that only understand
// API-key auth use this accessor instead of reaching into Auth.
func (f File) APIKey() string {
	if f.Auth != nil && f.Auth.Type == AuthAPIKey {
		return f.Auth.APIKey
	}
	return ""
}

// UnmarshalJSON parses the file and migrates the pre-version flat shape
// — {"region":...,"apiKey":...} — by folding a top-level apiKey into the
// auth block. New files never write a top-level apiKey; this only matters
// for a config written by an earlier build.
func (f *File) UnmarshalJSON(b []byte) error {
	type fileAlias File // strips methods to avoid recursion
	var raw struct {
		fileAlias
		LegacyAPIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*f = File(raw.fileAlias)
	if f.Auth == nil && raw.LegacyAPIKey != "" {
		f.Auth = &Auth{Type: AuthAPIKey, APIKey: raw.LegacyAPIKey}
	}
	return nil
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
	if f.Version == 0 {
		f.Version = Version
	}
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
