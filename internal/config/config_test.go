package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	want := File{Region: "eu", APIKey: "sk_live_abc123"}

	if err := saveTo(path, want); err != nil {
		t.Fatalf("saveTo: %v", err)
	}
	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
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
	if err := saveTo(path, File{APIKey: "sk_secret"}); err != nil {
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
	if err := saveTo(path, File{Region: "us", APIKey: "sk_one"}); err != nil {
		t.Fatal(err)
	}
	if err := saveTo(path, File{Region: "eu", APIKey: "sk_two"}); err != nil {
		t.Fatal(err)
	}
	got, err := loadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := (File{Region: "eu", APIKey: "sk_two"}); got != want {
		t.Errorf("after overwrite = %+v, want %+v", got, want)
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
