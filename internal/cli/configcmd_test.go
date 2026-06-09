package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/extend-hq/extend-cli/internal/config"
)

// TestConfigShowsResolvedSources verifies `extend config` reports each
// value with its source, and that an environment variable shadowing the
// saved file is surfaced as such. Fully hermetic: a temp XDG dir and
// t.Setenv-controlled environment, no network.
func TestConfigShowsResolvedSources(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("EXTEND_API_KEY", "sk_env_abcdefgh")
	t.Setenv("EXTEND_REGION", "")
	t.Setenv("EXTEND_BASE_URL", "")
	t.Setenv("EXTEND_WORKSPACE_ID", "")

	// The file supplies region; its key is shadowed by the env var above.
	if _, err := config.Save(config.File{
		Region: "eu",
		Auth:   &config.Auth{Type: config.AuthAPIKey, APIKey: "sk_file_value"},
	}); err != nil {
		t.Fatal(err)
	}

	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "config")
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config: %v", err)
	}
	out := ta.out.String()

	for _, want := range []string{
		"Auth method", "API key",
		"env: EXTEND_API_KEY",         // key resolved from env, shadowing the file
		"Region", "eu", "config file", // region resolved from the file
		"derived from region", // base URL follows the region
		"(not set)",           // workspace unset
	} {
		if !strings.Contains(out, want) {
			t.Errorf("config output missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "sk_env_abcdefgh") || strings.Contains(out, "sk_file_value") {
		t.Errorf("API key leaked unmasked; got:\n%s", out)
	}
}

// TestConfigReportsUnreadableConfigFile: when the config file exists but
// can't be parsed, `extend config` must say so instead of the misleading
// blanket "loaded" (which previously came from a bare os.Stat). This is the
// view that explains "the file is right there but the key is (not set)".
func TestConfigReportsUnreadableConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("EXTEND_API_KEY", "")
	t.Setenv("EXTEND_REGION", "")
	t.Setenv("EXTEND_BASE_URL", "")
	t.Setenv("EXTEND_WORKSPACE_ID", "")

	// Write a malformed config at the path the CLI will read.
	path := filepath.Join(dir, "extend", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "config")
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config: %v", err)
	}
	out := ta.out.String()

	if !strings.Contains(out, path) {
		t.Errorf("config output missing the config path %q; got:\n%s", path, out)
	}
	if !strings.Contains(out, "could not be read") {
		t.Errorf("config output should flag the unreadable file; got:\n%s", out)
	}
	// It must not pretend the broken file loaded cleanly.
	if strings.Contains(out, "(loaded)") {
		t.Errorf("config output should not claim the malformed file loaded; got:\n%s", out)
	}
}

func TestMaskKey(t *testing.T) {
	cases := map[string]string{
		"":                "(not set)",
		"short":           "•••••",
		"sk_env_abcdefgh": "sk_e…",
	}
	for in, want := range cases {
		if got := maskKey(in); got != want {
			t.Errorf("maskKey(%q) = %q, want %q", in, got, want)
		}
	}
}
