package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/extend-hq/extend-cli/internal/config"
)

// Record is one stored login, keyed by the API base URL it targets so
// logins against multiple regions or test rigs coexist. The endpoints
// and client id captured at login time ride along so refresh and revoke
// never need to re-discover or re-resolve them.
type Record struct {
	AccessToken        string    `json:"accessToken"`
	RefreshToken       string    `json:"refreshToken"`
	ExpiresAt          time.Time `json:"expiresAt"`
	TokenEndpoint      string    `json:"tokenEndpoint"`
	RevocationEndpoint string    `json:"revocationEndpoint"`
	ClientID           string    `json:"clientId"`
	// Resource is the exact RFC 8707 resource value sent at login,
	// echoed on every refresh.
	Resource string `json:"resource"`
}

// Store persists login records per API base URL. Get returns (nil, nil)
// when no record exists; Delete of an absent record is not an error.
type Store interface {
	Get(apiBase string) (*Record, error)
	Set(apiBase string, rec Record) error
	Delete(apiBase string) error
}

// EnvNoKeyring, when set truthy, forces the file store even where an OS
// keychain is available. Used by tests and headless rigs that cannot
// (or should not) touch the machine keychain.
const EnvNoKeyring = "EXTEND_OAUTH_NO_KEYRING"

// keyringService is the service name under which login records are
// stored in the OS keychain. The account name is the API base URL.
const keyringService = "extend-cli"

// DefaultStore returns the production store: the OS keychain, with a
// 0600 file under the config directory as fallback. The fallback exists
// for headless Linux hosts (and containers) without a Secret Service
// D-Bus (desktop bus) daemon, where go-keyring cannot reach a keychain
// at all; tokens still need to live somewhere, so they go next to the
// config file with owner-only permissions, matching how the config file
// already stores API keys.
func DefaultStore() Store {
	fs := &fileStore{}
	if v := os.Getenv(EnvNoKeyring); v != "" && v != "0" && v != "false" {
		return fs
	}
	return &keyringStore{fallback: fs}
}

// keyringStore stores one JSON-encoded Record per API base URL in the
// OS keychain, delegating to the file fallback when the keychain is
// unreachable.
type keyringStore struct {
	fallback *fileStore
}

func (s *keyringStore) Get(apiBase string) (*Record, error) {
	val, err := keyring.Get(keyringService, NormalizeBase(apiBase))
	if errors.Is(err, keyring.ErrNotFound) {
		// A login may have been written through the fallback earlier
		// (or by a headless session on the same home directory).
		return s.fallback.Get(apiBase)
	}
	if err != nil {
		return s.fallback.Get(apiBase)
	}
	var rec Record
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return nil, fmt.Errorf("parse stored login: %w", err)
	}
	return &rec, nil
}

func (s *keyringStore) Set(apiBase string, rec Record) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode login: %w", err)
	}
	if err := keyring.Set(keyringService, NormalizeBase(apiBase), string(b)); err != nil {
		return s.fallback.Set(apiBase, rec)
	}
	// A keychain write supersedes any stale fallback copy.
	_ = s.fallback.Delete(apiBase)
	return nil
}

func (s *keyringStore) Delete(apiBase string) error {
	err := keyring.Delete(keyringService, NormalizeBase(apiBase))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		// Keychain unreachable; the record, if any, lives in the file.
		return s.fallback.Delete(apiBase)
	}
	// Clear both places so a fallback copy cannot resurrect the login.
	if ferr := s.fallback.Delete(apiBase); ferr != nil {
		return ferr
	}
	return nil
}

// fileStore keeps every record in one 0600 JSON file next to the CLI
// config file. Writes are atomic (temp file + rename), mirroring
// internal/config.
type fileStore struct{}

// tokensFile is the on-disk shape of the fallback file.
type tokensFile struct {
	Version int               `json:"version"`
	Tokens  map[string]Record `json:"tokens"`
}

func tokensPath() (string, error) {
	cfgPath, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfgPath), "oauth_tokens.json"), nil
}

func (s *fileStore) load() (tokensFile, string, error) {
	path, err := tokensPath()
	if err != nil {
		return tokensFile{}, "", err
	}
	// Refuse to read through a symlink: the tokens file lives in a
	// user-owned directory, but a planted link could otherwise trick
	// the CLI into reading (and later atomically replacing) a file
	// elsewhere.
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return tokensFile{}, "", fmt.Errorf("%s is a symbolic link; refusing to read it", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tokensFile{Version: 1, Tokens: map[string]Record{}}, path, nil
		}
		return tokensFile{}, "", err
	}
	var f tokensFile
	if err := json.Unmarshal(b, &f); err != nil {
		return tokensFile{}, "", fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Tokens == nil {
		f.Tokens = map[string]Record{}
	}
	return f, path, nil
}

func (s *fileStore) save(path string, f tokensFile) error {
	if f.Version == 0 {
		f.Version = 1
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tokens: %w", err)
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, ".oauth-tokens-*.json")
	if err != nil {
		return fmt.Errorf("create temp tokens file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp tokens file: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp tokens file: %w", err)
	}
	// Flush to disk before the rename: without it a crash can install
	// an empty file over the previous tokens, losing the login.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp tokens file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp tokens file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install tokens file: %w", err)
	}
	return nil
}

func (s *fileStore) Get(apiBase string) (*Record, error) {
	f, _, err := s.load()
	if err != nil {
		return nil, err
	}
	rec, ok := f.Tokens[NormalizeBase(apiBase)]
	if !ok {
		return nil, nil
	}
	return &rec, nil
}

func (s *fileStore) Set(apiBase string, rec Record) error {
	f, path, err := s.load()
	if err != nil {
		return err
	}
	f.Tokens[NormalizeBase(apiBase)] = rec
	return s.save(path, f)
}

func (s *fileStore) Delete(apiBase string) error {
	f, path, err := s.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	key := NormalizeBase(apiBase)
	if _, ok := f.Tokens[key]; !ok {
		return nil
	}
	delete(f.Tokens, key)
	return s.save(path, f)
}
