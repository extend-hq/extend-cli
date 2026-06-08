package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	want := File{
		Region:      "eu",
		BaseURL:     "https://api.eu1.extend.ai",
		WorkspaceID: "ws_123",
		Auth:        &Auth{Type: AuthAPIKey, APIKey: "sk_live_abc123"},
	}

	if err := saveTo(path, want); err != nil {
		t.Fatalf("saveTo: %v", err)
	}
	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got.Region != want.Region || got.BaseURL != want.BaseURL || got.WorkspaceID != want.WorkspaceID {
		t.Errorf("round trip routing = %+v, want %+v", got, want)
	}
	if got.APIKey() != want.APIKey() {
		t.Errorf("round trip key = %q, want %q", got.APIKey(), want.APIKey())
	}
	if got.Version != Version {
		t.Errorf("round trip stamped version = %d, want %d", got.Version, Version)
	}
}

func TestLoadMissingReturnsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom on missing file should not error, got %v", err)
	}
	if (got != File{}) {
		t.Errorf("missing file should yield zero File, got %+v", got)
	}
}

func TestLoadMalformedErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFrom(path); err == nil {
		t.Error("expected error parsing malformed config, got nil")
	}
}

func TestSavePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions not meaningful on windows")
	}
	dir := filepath.Join(t.TempDir(), "extend")
	path := filepath.Join(dir, "config.json")
	if err := saveTo(path, File{Auth: &Auth{Type: AuthAPIKey, APIKey: "sk_secret"}}); err != nil {
		t.Fatalf("saveTo: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %o, want 600 (it holds a credential)", perm)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir mode = %o, want 700", perm)
	}
}

func TestSaveOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveTo(path, File{Region: "us", Auth: &Auth{Type: AuthAPIKey, APIKey: "sk_one"}}); err != nil {
		t.Fatal(err)
	}
	if err := saveTo(path, File{Region: "eu", Auth: &Auth{Type: AuthAPIKey, APIKey: "sk_two"}}); err != nil {
		t.Fatal(err)
	}
	got, err := loadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Region != "eu" || got.APIKey() != "sk_two" {
		t.Errorf("after overwrite = %+v, want region=eu key=sk_two", got)
	}
}

// TestLoadMigratesLegacyFlatShape ensures a config written by a pre-version
// build — a top-level apiKey rather than an auth block — still loads, with
// the key folded into Auth so the rest of the CLI sees it uniformly.
func TestLoadMigratesLegacyFlatShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"region":"us","apiKey":"sk_old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got.Region != "us" {
		t.Errorf("region = %q, want us", got.Region)
	}
	if got.APIKey() != "sk_old" {
		t.Errorf("migrated key = %q, want sk_old", got.APIKey())
	}
	if got.Auth == nil || got.Auth.Type != AuthAPIKey {
		t.Errorf("auth = %+v, want type=api_key", got.Auth)
	}
}

// TestSaveStampsVersionAndNestsKey ensures Save stamps the schema version
// and nests the credential under auth (never a top-level apiKey), keeping
// the file forward-compatible with future auth methods.
func TestSaveStampsVersionAndNestsKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveTo(path, File{Region: "us", Auth: &Auth{Type: AuthAPIKey, APIKey: "sk_x"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["apiKey"]; ok {
		t.Error("top-level apiKey written; key must be nested under auth")
	}
	if _, ok := raw["auth"]; !ok {
		t.Error("auth block missing from written file")
	}
	if string(raw["version"]) != "1" {
		t.Errorf("version field = %s, want 1", raw["version"])
	}
}

func TestPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-home")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/xdg-home", "extend", "config.json"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathFallsBackToHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-based fallback assertion is unix-specific")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/tester")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/tester", ".config", "extend", "config.json"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}
