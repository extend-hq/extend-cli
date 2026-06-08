package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfig_ReportsResolvedSources runs `extend config` fully offline
// (no API key required, no network) against a temp config dir, and
// asserts it reports each value's source — including an environment
// variable shadowing the saved file — with the API key masked.
func TestConfig_ReportsResolvedSources(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, "extend")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"version":1,"region":"eu","auth":{"type":"api_key","apiKey":"sk_file_key"}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	res := runExtendBare(t, map[string]string{
		"XDG_CONFIG_HOME": home,
		"EXTEND_API_KEY":  "sk_env_key",
	}, "config")
	res.requireOK(t, "config")

	out := string(res.Stdout)
	for _, want := range []string{
		"env: EXTEND_API_KEY",         // the env key shadows the file's key
		"Region", "eu", "config file", // region resolved from the file
		"derived from region", // base URL follows the region
	} {
		if !strings.Contains(out, want) {
			t.Errorf("`extend config` output missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "sk_env_key") || strings.Contains(out, "sk_file_key") {
		t.Errorf("API key leaked unmasked; got:\n%s", out)
	}
}
