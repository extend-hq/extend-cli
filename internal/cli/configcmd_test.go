package cli

import (
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

func TestMaskKey(t *testing.T) {
	cases := map[string]string{
		"":                "(not set)",
		"short":           "•••••",
		"sk_env_abcdefgh": "sk_e…efgh",
	}
	for in, want := range cases {
		if got := maskKey(in); got != want {
			t.Errorf("maskKey(%q) = %q, want %q", in, got, want)
		}
	}
}
