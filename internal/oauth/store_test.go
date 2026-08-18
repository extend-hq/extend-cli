package oauth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func testRecord() Record {
	return Record{
		AccessToken:        "eoat_access",
		RefreshToken:       "eort_refresh",
		ExpiresAt:          time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		TokenEndpoint:      "https://api.extend.ai/oauth2/token",
		RevocationEndpoint: "https://api.extend.ai/oauth2/revoke",
		ClientID:           "extend-cli",
		Resource:           "https://api.extend.ai",
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := &fileStore{}
	base := "https://api.extend.ai"

	if rec, err := s.Get(base); err != nil || rec != nil {
		t.Fatalf("Get before Set = (%v, %v), want (nil, nil)", rec, err)
	}

	want := testRecord()
	if err := s.Set(base+"/", want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Read back through the un-slashed key: normalization must make
	// both spellings the same login.
	got, err := s.Get(base)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || *got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}

	if err := s.Delete(base); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if rec, err := s.Get(base); err != nil || rec != nil {
		t.Errorf("Get after Delete = (%v, %v), want (nil, nil)", rec, err)
	}
	// Deleting again is not an error.
	if err := s.Delete(base); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}

func TestFileStoreKeysByBase(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := &fileStore{}

	us := testRecord()
	eu := testRecord()
	eu.AccessToken = "eoat_eu"
	eu.Resource = "https://api.eu1.extend.ai"

	if err := s.Set("https://api.extend.ai", us); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("https://api.eu1.extend.ai", eu); err != nil {
		t.Fatal(err)
	}

	gotUS, err := s.Get("https://api.extend.ai")
	if err != nil || gotUS == nil || gotUS.AccessToken != "eoat_access" {
		t.Errorf("us record = %+v, %v", gotUS, err)
	}
	gotEU, err := s.Get("https://api.eu1.extend.ai")
	if err != nil || gotEU == nil || gotEU.AccessToken != "eoat_eu" {
		t.Errorf("eu record = %+v, %v", gotEU, err)
	}
}

func TestFileStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on windows")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	s := &fileStore{}
	if err := s.Set("https://api.extend.ai", testRecord()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "extend", "oauth_tokens.json"))
	if err != nil {
		t.Fatalf("stat tokens file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("tokens file mode = %o, want 600", perm)
	}
}

func TestDefaultStoreHonorsNoKeyringEnv(t *testing.T) {
	t.Setenv(EnvNoKeyring, "1")
	if _, ok := DefaultStore().(*fileStore); !ok {
		t.Errorf("DefaultStore with %s=1 should be the file store", EnvNoKeyring)
	}
	t.Setenv(EnvNoKeyring, "")
	if _, ok := DefaultStore().(*keyringStore); !ok {
		t.Errorf("DefaultStore should default to the keyring store")
	}
}

func TestKeyringStoreRoundTrip(t *testing.T) {
	// MockInit swaps in an in-memory provider so tests never touch the
	// real OS keychain.
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := &keyringStore{fallback: &fileStore{}}
	base := "https://api.extend.ai"

	want := testRecord()
	if err := s.Set(base, want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(base)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
	if err := s.Delete(base); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if rec, err := s.Get(base); err != nil || rec != nil {
		t.Errorf("Get after Delete = (%v, %v), want (nil, nil)", rec, err)
	}
}

func TestKeyringStoreFallsBackToFile(t *testing.T) {
	keyring.MockInitWithError(os.ErrPermission)
	t.Cleanup(keyring.MockInit)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := &keyringStore{fallback: &fileStore{}}
	base := "https://api.extend.ai"

	want := testRecord()
	if err := s.Set(base, want); err != nil {
		t.Fatalf("Set with broken keyring should fall back to file: %v", err)
	}
	// The record must be readable both through the combined store and
	// directly from the file fallback.
	got, err := s.Get(base)
	if err != nil || got == nil || got.AccessToken != want.AccessToken {
		t.Errorf("combined Get = %+v, %v", got, err)
	}
	fromFile, err := (&fileStore{}).Get(base)
	if err != nil || fromFile == nil {
		t.Errorf("file fallback Get = %+v, %v", fromFile, err)
	}
	if err := s.Delete(base); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if rec, _ := (&fileStore{}).Get(base); rec != nil {
		t.Errorf("file fallback should be cleared after Delete, got %+v", rec)
	}
}
