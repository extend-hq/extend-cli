package cli

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/extend-hq/extend-cli/internal/config"
	"github.com/extend-hq/extend-cli/internal/extendx"
)

// TestResolveSettings pins the precedence chain that makes `extend setup`
// actually wire every command: flag/env win, the config file is the
// lowest-priority fallback, and an --env label scopes only the API key
// (the file's region still applies under any label).
func TestResolveSettings(t *testing.T) {
	file := config.File{Region: "eu", Auth: &config.Auth{Type: config.AuthAPIKey, APIKey: "sk_file"}}
	loadFile := func() (config.File, error) { return file, nil }
	loadEmpty := func() (config.File, error) { return config.File{}, nil }
	fileWithBase := config.File{Region: "eu", BaseURL: "https://self.example", Auth: &config.Auth{Type: config.AuthAPIKey, APIKey: "sk_file"}}
	loadWithBase := func() (config.File, error) { return fileWithBase, nil }
	fileWithWorkspace := config.File{Region: "eu", WorkspaceID: "ws_file", Auth: &config.Auth{Type: config.AuthAPIKey, APIKey: "sk_file"}}
	loadWithWorkspace := func() (config.File, error) { return fileWithWorkspace, nil }

	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cases := []struct {
		name            string
		envLabel        string
		regionFlag      string
		workspaceFlag   string
		getenv          func(string) string
		load            func() (config.File, error)
		wantKey         string
		wantRegion      string
		wantBaseURL     string
		wantWorkspaceID string
		wantAPIVersion  string
	}{
		{
			name:       "env key wins over file",
			getenv:     env(map[string]string{"EXTEND_API_KEY": "sk_env"}),
			load:       loadFile,
			wantKey:    "sk_env",
			wantRegion: "eu", // region only in file
		},
		{
			name:       "file key used when env empty",
			getenv:     env(map[string]string{}),
			load:       loadFile,
			wantKey:    "sk_file",
			wantRegion: "eu",
		},
		{
			name:       "region flag wins over env and file",
			regionFlag: "us2",
			getenv:     env(map[string]string{"EXTEND_REGION": "us"}),
			load:       loadFile,
			wantKey:    "sk_file",
			wantRegion: "us2",
		},
		{
			name:       "region env wins over file",
			getenv:     env(map[string]string{"EXTEND_REGION": "us"}),
			load:       loadFile,
			wantKey:    "sk_file",
			wantRegion: "us",
		},
		{
			name:       "env label scopes the key but file region still applies",
			envLabel:   "test",
			getenv:     env(map[string]string{"EXTEND_TEST_API_KEY": "sk_test"}),
			load:       loadFile,
			wantKey:    "sk_test", // labeled key from env, never the file's default key
			wantRegion: "eu",      // region is not env-specific; file value still used
		},
		{
			name:       "env label with no labeled key does not borrow the file's default key",
			envLabel:   "test",
			getenv:     env(map[string]string{}), // EXTEND_TEST_API_KEY unset
			load:       loadFile,
			wantKey:    "",   // file's default key must not leak into a labeled env
			wantRegion: "eu", // region still applies
		},
		{
			name:       "nothing set yields empty key",
			getenv:     env(map[string]string{}),
			load:       loadEmpty,
			wantKey:    "",
			wantRegion: "",
		},
		{
			name:        "base url from file when env unset",
			getenv:      env(map[string]string{}),
			load:        loadWithBase,
			wantKey:     "sk_file",
			wantRegion:  "eu",
			wantBaseURL: "https://self.example",
		},
		{
			name:        "base url env wins over file",
			getenv:      env(map[string]string{"EXTEND_BASE_URL": "https://env.example"}),
			load:        loadWithBase,
			wantKey:     "sk_file",
			wantRegion:  "eu",
			wantBaseURL: "https://env.example",
		},
		{
			name:        "file base url applies under an env label too",
			envLabel:    "test",
			getenv:      env(map[string]string{"EXTEND_TEST_API_KEY": "sk_test"}),
			load:        loadWithBase,
			wantKey:     "sk_test",
			wantRegion:  "eu",
			wantBaseURL: "https://self.example",
		},
		{
			name:            "workspace flag wins over env and file",
			workspaceFlag:   "ws_flag",
			getenv:          env(map[string]string{"EXTEND_WORKSPACE_ID": "ws_env"}),
			load:            loadWithWorkspace,
			wantKey:         "sk_file",
			wantRegion:      "eu",
			wantWorkspaceID: "ws_flag",
		},
		{
			name:            "workspace env wins over file",
			getenv:          env(map[string]string{"EXTEND_WORKSPACE_ID": "ws_env"}),
			load:            loadWithWorkspace,
			wantKey:         "sk_file",
			wantRegion:      "eu",
			wantWorkspaceID: "ws_env",
		},
		{
			name:            "workspace from file when flag and env unset",
			getenv:          env(map[string]string{}),
			load:            loadWithWorkspace,
			wantKey:         "sk_file",
			wantRegion:      "eu",
			wantWorkspaceID: "ws_file",
		},
		{
			name:            "file workspace applies under an env label too",
			envLabel:        "test",
			getenv:          env(map[string]string{"EXTEND_TEST_API_KEY": "sk_test"}),
			load:            loadWithWorkspace,
			wantKey:         "sk_test",
			wantRegion:      "eu",
			wantWorkspaceID: "ws_file",
		},
		{
			name:           "api version comes from its env var",
			getenv:         env(map[string]string{"EXTEND_API_VERSION": "2025-04-21"}),
			load:           loadEmpty,
			wantAPIVersion: "2025-04-21",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSettings(tc.envLabel, tc.regionFlag, tc.workspaceFlag, tc.getenv, tc.load)
			if got.key.val != tc.wantKey {
				t.Errorf("key = %q, want %q", got.key.val, tc.wantKey)
			}
			if got.region.val != tc.wantRegion {
				t.Errorf("region = %q, want %q", got.region.val, tc.wantRegion)
			}
			if got.baseURL.val != tc.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", got.baseURL.val, tc.wantBaseURL)
			}
			if got.workspaceID.val != tc.wantWorkspaceID {
				t.Errorf("workspaceID = %q, want %q", got.workspaceID.val, tc.wantWorkspaceID)
			}
			if got.apiVersion.val != tc.wantAPIVersion {
				t.Errorf("apiVersion = %q, want %q", got.apiVersion.val, tc.wantAPIVersion)
			}
			if got.fileErr != nil {
				t.Errorf("fileErr = %v, want nil for a clean load", got.fileErr)
			}
		})
	}
}

// TestResolveSettingsSurfacesLoadError: a config file that exists but can't
// be read or parsed must not be swallowed. resolveSettings reports the error
// via fileErr (so `extend config` and the "key not set" error can explain
// it) while still falling back gracefully: the key stays empty rather than
// the whole command crashing on a malformed file.
func TestResolveSettingsSurfacesLoadError(t *testing.T) {
	loadErr := errors.New("parse config.json: unexpected end of JSON input")
	loadBroken := func() (config.File, error) { return config.File{}, loadErr }

	got := resolveSettings("", "", "", func(string) string { return "" }, loadBroken)
	if got.fileErr == nil {
		t.Fatal("fileErr = nil, want the load error surfaced")
	}
	if !errors.Is(got.fileErr, loadErr) {
		t.Errorf("fileErr = %v, want it to wrap %v", got.fileErr, loadErr)
	}
	if got.key.val != "" {
		t.Errorf("key = %q, want empty when the file failed to load", got.key.val)
	}
}

// TestRunSetupNonInteractive_Unconfigured: with no resolvable key and no
// TTY, setup prints copy-pasteable guidance to stdout (the relayable
// channel), names both regional dashboards, never claims to be configured,
// and exits nil. Skill install is skipped here (via the flag) to keep the
// test off the home dir.
func TestRunSetupNonInteractive_Unconfigured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(envAPIKey, "")
	t.Setenv(envRegion, "")

	ta := newTestApp(t, newFakeServer(t, nil))
	if err := runSetupNonInteractive(ta.app, setupOptions{skipSkillInstall: true}); err != nil {
		t.Fatalf("runSetupNonInteractive = %v, want nil", err)
	}

	out := ta.out.String()
	for _, want := range []string{
		"not configured",
		"export " + envAPIKey,
		"https://dashboard.extend.ai",
		"https://dashboard.eu1.extend.ai",
		"extend config",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "already configured") {
		t.Errorf("unconfigured run must not claim configured; got:\n%s", out)
	}
	if errs := ta.errOut.String(); !strings.Contains(errs, "Skipping agent skill") {
		t.Errorf("stderr missing skip note; got:\n%s", errs)
	}
}

// TestRunSetupNonInteractive_Configured: when a key already resolves, setup
// confirms it on stderr (with the region), prints nothing to stdout, and
// never echoes the key. EXTEND_SKIP_SKILL_INSTALL is set here to assert
// the env-fallback path still suppresses the skill install.
func TestRunSetupNonInteractive_Configured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(envAPIKey, "sk_live_test")
	t.Setenv(envRegion, "eu")
	t.Setenv(envSkipSkillInstall, "1")

	ta := newTestApp(t, newFakeServer(t, nil))
	if err := runSetupNonInteractive(ta.app, setupOptions{}); err != nil {
		t.Fatalf("runSetupNonInteractive = %v, want nil", err)
	}

	if out := ta.out.String(); out != "" {
		t.Errorf("configured run must print no guidance to stdout; got:\n%s", out)
	}
	errs := ta.errOut.String()
	if !strings.Contains(errs, "already configured") || !strings.Contains(errs, "region eu") {
		t.Errorf("stderr should confirm configuration with region; got:\n%s", errs)
	}
	if strings.Contains(errs, "sk_live_test") || strings.Contains(ta.out.String(), "sk_live_test") {
		t.Error("API key value leaked into output")
	}
}

// TestRunSetupNonInteractive_InstallsSkill: without the skip knob, the
// non-interactive path writes the skill to its default location and
// symlinks it into ~/.claude/skills.
func TestRunSetupNonInteractive_InstallsSkill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink assertions are unix-specific")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(envAPIKey, "sk_x") // configured: keep stdout quiet, focus on the skill
	t.Setenv(envSkipSkillInstall, "")

	ta := newTestApp(t, newFakeServer(t, nil))
	if err := runSetupNonInteractive(ta.app, setupOptions{}); err != nil {
		t.Fatalf("runSetupNonInteractive = %v, want nil", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "extend", "SKILL.md")); err != nil {
		t.Fatalf("skill not installed to default location: %v", err)
	}
	link := filepath.Join(home, ".claude", "skills", "extend")
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("claude symlink not created (fi=%v err=%v)", fi, err)
	}
	if errs := ta.errOut.String(); !strings.Contains(errs, "Installed the Extend agent skill") || !strings.Contains(errs, "Linked") {
		t.Errorf("stderr missing install/link status; got:\n%s", errs)
	}
}

// TestRunSetupNonInteractive_ConfiguredCustomBaseURL: when a custom base
// URL determines routing, the confirmation reports it instead of claiming
// the default region.
func TestRunSetupNonInteractive_ConfiguredCustomBaseURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(envAPIKey, "sk_x")
	t.Setenv(envRegion, "")
	t.Setenv(envBaseURL, "https://self.example")

	ta := newTestApp(t, newFakeServer(t, nil))
	if err := runSetupNonInteractive(ta.app, setupOptions{skipSkillInstall: true}); err != nil {
		t.Fatalf("runSetupNonInteractive = %v, want nil", err)
	}
	errs := ta.errOut.String()
	if !strings.Contains(errs, "base URL https://self.example") {
		t.Errorf("stderr should report the custom base URL; got:\n%s", errs)
	}
	if strings.Contains(errs, "region us (default)") {
		t.Errorf("must not claim default region when a base URL is set; got:\n%s", errs)
	}
}

// TestForcedNonInteractive pins the TTY-override escape hatches that keep
// the wizard from blocking on a human-less pty. The --non-interactive
// flag wins over the env vars; the env vars are still honored when the
// flag is not set.
func TestForcedNonInteractive(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name string
		flag bool
		m    map[string]string
		want bool
	}{
		{"nothing set", false, nil, false},
		{"flag wins", true, nil, true},
		{"flag wins over off envs", true, map[string]string{"EXTEND_NONINTERACTIVE": "0"}, true},
		{"EXTEND_NONINTERACTIVE truthy", false, map[string]string{"EXTEND_NONINTERACTIVE": "1"}, true},
		{"CI truthy", false, map[string]string{"CI": "true"}, true},
		{"CI=0 is off", false, map[string]string{"CI": "0"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := forcedNonInteractive(tc.flag, env(tc.m)); got != tc.want {
				t.Errorf("forcedNonInteractive = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveSkipSkill pins the precedence for the agent-skill-install
// suppression knob: --skip-skill-install flag > EXTEND_SKIP_SKILL_INSTALL
// > default false.
func TestResolveSkipSkill(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name string
		flag bool
		m    map[string]string
		want bool
	}{
		{"nothing set", false, nil, false},
		{"flag wins", true, nil, true},
		{"flag wins over off env", true, map[string]string{"EXTEND_SKIP_SKILL_INSTALL": "0"}, true},
		{"env truthy", false, map[string]string{"EXTEND_SKIP_SKILL_INSTALL": "1"}, true},
		{"env false", false, map[string]string{"EXTEND_SKIP_SKILL_INSTALL": "0"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSkipSkill(tc.flag, env(tc.m)); got != tc.want {
				t.Errorf("resolveSkipSkill = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSetupModel_SkipSkillSkipsPrompt: with skipSkill set (from
// EXTEND_SKIP_SKILL_INSTALL), a successful validation finishes without the
// skill prompt and records installSkill=false.
func TestSetupModel_SkipSkillSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	m := newSetupModel(context.Background(), false, "eu", func(context.Context, string, string, string) error { return nil })
	m.region = setupRegionChoices[1] // eu
	m.apiKey = "sk_live_123"
	m.skipSkill = true
	m.step = stepValidating

	m = drive(t, m, validatedMsg{err: nil})
	if m.step != stepDone {
		t.Fatalf("step = %v, want stepDone (skill prompt skipped)", m.step)
	}
	if m.result == nil || m.result.installSkill {
		t.Errorf("installSkill should be false when skipSkill is set; result = %+v", m.result)
	}
}

// TestSetupModel_EscOnKeyStepSkipsSaving: esc at the key step is the
// opt-out the note advertises. It must leave the disk untouched, record
// saved=false, and continue to the skill prompt; the summary then prints
// env-var guidance.
func TestSetupModel_EscOnKeyStepSkipsSaving(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	m := newSetupModel(context.Background(), false, "eu", func(context.Context, string, string, string) error { return nil })
	m.region = setupRegionChoices[1] // eu
	m.step = stepKey

	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.step != stepSkill {
		t.Fatalf("step = %v, want stepSkill (skip still continues)", m.step)
	}
	if m.result == nil || m.result.saved || m.result.path != "" {
		t.Fatalf("result = %+v, want saved=false with no path", m.result)
	}
	if post, err := config.Load(); err != nil || post.APIKey() != "" {
		t.Fatalf("config written despite skip: %+v (err=%v)", post, err)
	}
}

// TestSetupModel_KeyStepShowsSaveNote: the key-entry screen is the
// transparency moment: it must say where a validated key will be saved
// and that esc skips the save (env var as the alternative).
func TestSetupModel_KeyStepShowsSaveNote(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	m := newSetupModel(context.Background(), false, "us", func(context.Context, string, string, string) error { return nil })
	m.region = setupRegionChoices[0]
	m.step = stepKey

	body := m.renderStep()
	for _, want := range []string{
		filepath.Join(dir, "extend", "config.json"), // the exact destination
		"(optional)",     // the key can be skipped
		"esc",            // the skip
		"EXTEND_API_KEY", // the alternative
	} {
		if !strings.Contains(body, want) {
			t.Errorf("key step missing %q; got:\n%s", want, body)
		}
	}
}

// TestReportSetupResult pins the post-wizard summary both ways: a saved
// result reports the path; a declined save prints copy-pasteable env-var
// guidance (region and workspace included only when relevant) and never
// echoes the key itself.
func TestReportSetupResult(t *testing.T) {
	t.Run("saved", func(t *testing.T) {
		ta := newTestApp(t, newFakeServer(t, nil))
		res := &setupResult{region: setupRegionChoices[1], apiKey: "sk_z", saved: true, path: "/tmp/cfg.json"}
		if err := reportSetupResult(ta.app, res); err != nil {
			t.Fatalf("reportSetupResult = %v", err)
		}
		out := ta.errOut.String()
		if !strings.Contains(out, "/tmp/cfg.json") {
			t.Errorf("saved summary missing path; got:\n%s", out)
		}
		if strings.Contains(out, "sk_z") {
			t.Errorf("summary must never echo the key; got:\n%s", out)
		}
	})

	t.Run("skipped", func(t *testing.T) {
		ta := newTestApp(t, newFakeServer(t, nil))
		// esc-skip: no key was entered or validated, only region chosen.
		res := &setupResult{region: setupRegionChoices[1], workspaceID: "ws_1", saved: false}
		if err := reportSetupResult(ta.app, res); err != nil {
			t.Fatalf("reportSetupResult = %v", err)
		}
		out := ta.errOut.String()
		for _, want := range []string{
			"export EXTEND_API_KEY=",
			"export EXTEND_REGION=eu",
			"export EXTEND_WORKSPACE_ID=ws_1",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("skipped summary missing %q; got:\n%s", want, out)
			}
		}
		if strings.Contains(out, "validated") {
			t.Errorf("skipped summary must not claim a key was validated; got:\n%s", out)
		}
	})

	t.Run("skipped us region omits region line", func(t *testing.T) {
		ta := newTestApp(t, newFakeServer(t, nil))
		res := &setupResult{region: setupRegionChoices[0], saved: false}
		if err := reportSetupResult(ta.app, res); err != nil {
			t.Fatalf("reportSetupResult = %v", err)
		}
		if out := ta.errOut.String(); strings.Contains(out, "EXTEND_REGION") {
			t.Errorf("us (default) region should not need an EXTEND_REGION line; got:\n%s", out)
		}
	})
}

// TestValidateAPIKey_OK confirms a 2xx from the workflows list endpoint
// is treated as a valid key.
func TestValidateAPIKey_OK(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/workflows" {
			writeJSON(w, 200, map[string]any{"data": []any{}})
			return
		}
		t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
	})
	err := validateAPIKey(context.Background(), extendx.Config{APIKey: "sk_ok", BaseURL: srv.URL()})
	if err != nil {
		t.Fatalf("validateAPIKey on 200 = %v, want nil", err)
	}
}

// TestValidateAPIKey_Unauthorized confirms a 401 surfaces as a typed API
// error the wizard can explain.
func TestValidateAPIKey_Unauthorized(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeAPIError(w, 401, "UNAUTHORIZED", "invalid api key")
	})
	err := validateAPIKey(context.Background(), extendx.Config{APIKey: "sk_bad", BaseURL: srv.URL()})
	if err == nil {
		t.Fatal("validateAPIKey on 401 = nil, want error")
	}
	status, ok := apiErrorStatus(err)
	if !ok || status != 401 {
		t.Fatalf("apiErrorStatus = (%d,%v), want (401,true)", status, ok)
	}
	if msg := setupValidationMessage(err); !strings.Contains(msg, "401") {
		t.Errorf("setupValidationMessage = %q, want it to mention 401", msg)
	}
}

func TestSetupValidationMessage(t *testing.T) {
	if got := setupValidationMessage(errEmptyWorkspace); got != "Please paste a workspace ID." {
		t.Errorf("empty-workspace message = %q", got)
	}
	if got := setupValidationMessage(nil); got != "" {
		t.Errorf("nil message = %q, want empty", got)
	}
}

// drive feeds a message to the model and returns the next concrete model.
func drive(t *testing.T, m setupModel, msg tea.Msg) setupModel {
	t.Helper()
	next, _ := m.Update(msg)
	sm, ok := next.(setupModel)
	if !ok {
		t.Fatalf("Update returned %T, want setupModel", next)
	}
	return sm
}

// TestSetupModel_RegionSelection walks the region picker: down moves the
// cursor, enter advances to the key step with the chosen region.
func TestSetupModel_RegionSelection(t *testing.T) {
	m := newSetupModel(context.Background(), false, "", func(context.Context, string, string, string) error { return nil })
	if m.step != stepRegion {
		t.Fatalf("initial step = %v, want stepRegion", m.step)
	}
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown}) // -> EU
	if m.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", m.cursor)
	}
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown}) // clamp at last
	if m.cursor != 1 {
		t.Fatalf("cursor clamped = %d, want 1", m.cursor)
	}
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.step != stepKey {
		t.Fatalf("step after enter = %v, want stepKey", m.step)
	}
	if m.region.id != "eu" {
		t.Errorf("region = %q, want eu", m.region.id)
	}
}

// TestSetupModel_EmptyKeySubmitSkips: the key is optional. Enter on an
// empty input skips saving exactly like esc: continue to the skill
// prompt, nothing written, env-var guidance in the summary.
func TestSetupModel_EmptyKeySubmitSkips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	m := newSetupModel(context.Background(), false, "us", func(context.Context, string, string, string) error { return nil })
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // pick US
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // submit empty key
	if m.step != stepSkill {
		t.Fatalf("step = %v, want stepSkill (empty key submits as skip)", m.step)
	}
	if m.result == nil || m.result.saved || m.result.apiKey != "" {
		t.Fatalf("result = %+v, want saved=false with no key", m.result)
	}
	if post, err := config.Load(); err != nil || post.APIKey() != "" {
		t.Fatalf("config written despite skip: %+v (err=%v)", post, err)
	}
}

// TestSetupModel_ValidateSuccessSaves drives the model to a successful
// validation and asserts the config file is written with the chosen
// region and key (the key step's note announces the save; esc there is
// the opt-out).
func TestSetupModel_ValidateSuccessSaves(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	m := newSetupModel(context.Background(), false, "eu", func(context.Context, string, string, string) error { return nil })
	m.region = setupRegionChoices[1] // eu
	m.apiKey = "sk_live_123"
	m.step = stepValidating

	m = drive(t, m, validatedMsg{err: nil})
	// On success the config is saved and the wizard advances to the
	// skill-install prompt (not straight to done).
	if m.step != stepSkill {
		t.Fatalf("step = %v, want stepSkill", m.step)
	}
	if m.result == nil || m.result.saveErr != nil || !m.result.saved {
		t.Fatalf("result = %+v, want a saved result with no error", m.result)
	}
	wantPath := filepath.Join(dir, "extend", "config.json")
	if m.result.path != wantPath {
		t.Errorf("saved path = %q, want %q", m.result.path, wantPath)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if saved.Region != "eu" || saved.APIKey() != "sk_live_123" {
		t.Errorf("saved config = %+v, want region=eu key=sk_live_123", saved)
	}
	if saved.Version != config.Version {
		t.Errorf("saved version = %d, want %d", saved.Version, config.Version)
	}
	if saved.Auth == nil || saved.Auth.Type != config.AuthAPIKey {
		t.Errorf("saved auth = %+v, want type=api_key", saved.Auth)
	}
}

// TestSetupModel_SkillPromptChoice covers the post-validation skill
// prompt: "Yes" records installSkill, "No" doesn't, and either way the
// wizard finishes.
func TestSetupModel_SkillPromptChoice(t *testing.T) {
	mk := func() setupModel {
		m := newSetupModel(context.Background(), false, "us", func(context.Context, string, string, string) error { return nil })
		m.region = setupRegionChoices[0]
		m.step = stepSkill
		m.result = &setupResult{region: m.region, apiKey: "sk_x", path: "/tmp/x"}
		return m
	}

	// Default cursor (0 = Yes) + enter installs.
	yes := drive(t, mk(), tea.KeyPressMsg{Code: tea.KeyEnter})
	if yes.step != stepDone {
		t.Errorf("after enter step = %v, want stepDone", yes.step)
	}
	if !yes.result.installSkill {
		t.Error("enter on default (Yes) should set installSkill=true")
	}

	// Pressing 'n' declines.
	no := drive(t, mk(), tea.KeyPressMsg{Code: 'n', Text: "n"})
	if no.step != stepDone {
		t.Errorf("after 'n' step = %v, want stepDone", no.step)
	}
	if no.result.installSkill {
		t.Error("'n' should set installSkill=false")
	}

	// Toggling down then enter also declines.
	m := mk()
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.result.installSkill {
		t.Error("toggle to No then enter should set installSkill=false")
	}
}

// TestSetupModel_ValidateFailureReturnsToKey ensures a rejected key sends
// the user back to the key step with the error shown, not a crash.
func TestSetupModel_ValidateFailureReturnsToKey(t *testing.T) {
	m := newSetupModel(context.Background(), false, "us", func(context.Context, string, string, string) error { return nil })
	m.region = setupRegionChoices[0]
	m.apiKey = "sk_bad"
	m.step = stepValidating

	boom := errors.New("boom")
	m = drive(t, m, validatedMsg{err: boom})
	if m.step != stepKey {
		t.Fatalf("step = %v, want stepKey", m.step)
	}
	if m.valErr != boom {
		t.Errorf("valErr = %v, want boom", m.valErr)
	}
	if m.result != nil {
		t.Errorf("result = %+v, want nil on failure", m.result)
	}
}

// TestSetupModel_OrgKeyPromptsForWorkspace covers the reactive workspace
// flow: a 400 "workspace required" routes the user to a workspace prompt
// (not a dead-end error), and a successful re-validation persists the
// workspace ID alongside the key.
func TestSetupModel_OrgKeyPromptsForWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	wsRequired := &extendx.APIError{
		StatusCode: 400,
		Message:    "X-Extend-Workspace-Id header is required for organization-level API keys.",
	}

	m := newSetupModel(context.Background(), false, "us", func(context.Context, string, string, string) error { return nil })
	m.region = setupRegionChoices[0] // us
	m.apiKey = "sk_org"
	m.step = stepValidating

	// Org key, no workspace yet → routed to the workspace step, no error.
	m = drive(t, m, validatedMsg{err: wsRequired})
	if m.step != stepWorkspace {
		t.Fatalf("step = %v, want stepWorkspace", m.step)
	}
	if m.valErr != nil {
		t.Errorf("valErr = %v, want nil (prompt, not error)", m.valErr)
	}

	// Enter a workspace ID and submit → re-validates with it.
	m.wsInput.SetValue("ws_123")
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.step != stepValidating {
		t.Fatalf("step after workspace submit = %v, want stepValidating", m.step)
	}
	if m.workspaceID != "ws_123" {
		t.Fatalf("workspaceID = %q, want ws_123", m.workspaceID)
	}

	// Successful validation persists region + key + workspace.
	m = drive(t, m, validatedMsg{err: nil})
	if m.step != stepSkill {
		t.Fatalf("step = %v, want stepSkill", m.step)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if saved.WorkspaceID != "ws_123" {
		t.Errorf("saved workspace = %q, want ws_123", saved.WorkspaceID)
	}
	if saved.APIKey() != "sk_org" {
		t.Errorf("saved key = %q, want sk_org", saved.APIKey())
	}
}

// TestSetupModel_ValidateCmdInvokesValidator confirms the command the
// model dispatches actually calls the injected validator with the chosen
// region and key.
func TestSetupModel_ValidateCmdInvokesValidator(t *testing.T) {
	var gotRegion, gotKey, gotWS string
	m := newSetupModel(context.Background(), false, "", func(_ context.Context, region, key, ws string) error {
		gotRegion, gotKey, gotWS = region, key, ws
		return nil
	})
	m.region = setupRegionChoices[1]
	m.apiKey = "sk_xyz"
	m.workspaceID = "ws_42"

	msg := m.validateCmd()()
	if _, ok := msg.(validatedMsg); !ok {
		t.Fatalf("validateCmd produced %T, want validatedMsg", msg)
	}
	if gotRegion != "eu" || gotKey != "sk_xyz" || gotWS != "ws_42" {
		t.Errorf("validator called with (%q,%q,%q), want (eu,sk_xyz,ws_42)", gotRegion, gotKey, gotWS)
	}
}

// TestSetupModel_CtrlCCancels ensures ctrl+c flags cancellation from any
// step.
func TestSetupModel_CtrlCCancels(t *testing.T) {
	m := newSetupModel(context.Background(), false, "", func(context.Context, string, string, string) error { return nil })
	m = drive(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.canceled {
		t.Error("ctrl+c should set canceled")
	}
}

// TestBuildLogoGrid confirms the embedded logo decodes and rasterizes to
// the requested width with lit cells (i.e. the asset is present and the
// renderer works).
func TestBuildLogoGrid(t *testing.T) {
	grid := buildLogoGrid(72)
	if len(grid) == 0 {
		t.Fatal("buildLogoGrid returned no rows; embedded logo failed to decode")
	}
	lit := 0
	for _, row := range grid {
		if len(row) != 72 {
			t.Fatalf("row width = %d, want 72", len(row))
		}
		for _, c := range row {
			if c.lit {
				lit++
			}
		}
	}
	if lit == 0 {
		t.Error("rasterized logo has no lit cells; threshold likely wrong")
	}
}

// TestSetupView_FitsScreenAndShowsBody is a regression test for the
// layout bug where the compositor canvas was sized to the union of layer
// bounds (not the screen), clipping the logo on the right and chopping
// the body content below the first line. The rendered View must be
// exactly the terminal size and must contain the full region-step body.
func TestSetupView_FitsScreenAndShowsBody(t *testing.T) {
	m := newSetupModel(context.Background(), true, "", func(context.Context, string, string, string) error { return nil })
	const w, h = 100, 40
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(setupModel)

	content := m.View().Content

	if gw := lipgloss.Width(content); gw > w {
		t.Errorf("view width = %d, want <= %d (content overflows screen)", gw, w)
	}
	if gh := lipgloss.Height(content); gh != h {
		t.Errorf("view height = %d, want exactly %d", gh, h)
	}

	// The whole region-step body must survive — not just the heading.
	for _, want := range []string{
		"Document Processing APIs",
		"Where is your Extend workspace?",
		"United States",
		"European Union",
		"move", // footer hint ("↑/↓ move · enter select · q quit")
	} {
		if !strings.Contains(content, want) {
			t.Errorf("rendered view is missing %q (content was clipped)", want)
		}
	}
}

// TestSetupView_NarrowAndTallStillFits guards the trim-from-top path: a
// short, narrow terminal must still produce output no wider/taller than
// the screen.
func TestSetupView_NarrowAndTallStillFits(t *testing.T) {
	m := newSetupModel(context.Background(), true, "", func(context.Context, string, string, string) error { return nil })
	const w, h = 60, 20
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(setupModel)

	content := m.View().Content
	if gw := lipgloss.Width(content); gw > w {
		t.Errorf("view width = %d, want <= %d", gw, w)
	}
	if gh := lipgloss.Height(content); gh != h {
		t.Errorf("view height = %d, want exactly %d", gh, h)
	}
}

// TestChompAimsLetterInkToMarkCenter verifies the chomp aims each
// letter's ink center at the mark's ink center (within the half-cell
// limit of the braille grid), with no systematic leftward bias.
func TestChompAimsLetterInkToMarkCenter(t *testing.T) {
	m := newSetupModel(context.Background(), true, "", func(context.Context, string, string, string) error { return nil })
	next, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 40})
	m = next.(setupModel)
	if len(m.regions) < 2 {
		t.Fatalf("expected mark + letters, got %d regions", len(m.regions))
	}
	markInk := m.markCenterCol()
	for i := 1; i < len(m.regions); i++ {
		r := m.regions[i]
		// Mirror the approach-target formula in advanceLogo.
		target := float64(r.StartCol) + math.Round(markInk-r.InkMid)
		landedInk := r.InkMid - float64(r.StartCol) + math.Round(target)
		if off := landedInk - markInk; math.Abs(off) > 0.5 {
			t.Errorf("letter %d lands ink at %.1f, mark ink %.1f (off %.1f > 0.5)", i, landedInk, markInk, off)
		}
	}
}

// brailleMaxCol returns the rightmost column holding a braille cell in a
// (ANSI-stripped) line, or -1.
func brailleMaxCol(line string) int {
	maxc, col := -1, 0
	for _, r := range line {
		if r >= 0x2800 && r <= 0x28FF {
			maxc = col
		}
		col++
	}
	return maxc
}

// TestScanReturnKeepsTextConsumed is a regression test: while the scan's
// mark glides back home, the wordmark must stay fully consumed (it must
// NOT un-wipe as the mark passes back over it). The letters only reappear
// later via the fly-in once the mark is home.
func TestScanReturnKeepsTextConsumed(t *testing.T) {
	m := newSetupModel(context.Background(), true, "", func(context.Context, string, string, string) error { return nil })
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = next.(setupModel)
	for i := 0; i < 200; i++ {
		next, _ = m.Update(logoTickMsg{})
		m = next.(setupModel)
	}
	m.startScan()
	m.scanSub = scanSubReturn
	m.scanMarkX = m.regions[0].InkMid + 20 // mark mid-return

	pad := (m.width - m.cols) / 2
	markRight := pad + int(m.scanMarkX) + m.chompMarkDims.canvasW/4 + 2

	got := -1
	for _, line := range strings.Split(ansi.Strip(m.renderScan()), "\n") {
		if c := brailleMaxCol(line); c > got {
			got = c
		}
	}
	if got > markRight {
		t.Errorf("wordmark visible during return: braille at col %d past mark right ~%d", got, markRight)
	}
}

// TestScanCompletesBackToIntro drives the whole scan effect and asserts
// it finishes by replaying the wordmark fly-in.
func TestScanCompletesBackToIntro(t *testing.T) {
	m := newSetupModel(context.Background(), true, "", func(context.Context, string, string, string) error { return nil })
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = next.(setupModel)
	for i := 0; i < 200; i++ {
		next, _ = m.Update(logoTickMsg{})
		m = next.(setupModel)
	}
	m.startScan()
	if m.phase != phaseScanning {
		t.Fatalf("phase=%v want phaseScanning", m.phase)
	}
	sawReturn := false
	done := false
	for i := 0; i < 2000; i++ {
		next, _ = m.Update(logoTickMsg{})
		m = next.(setupModel)
		if m.phase == phaseScanning && m.scanSub == scanSubReturn {
			sawReturn = true
		}
		if m.phase == phaseIntro || m.phase == phaseIdle {
			done = true
			break
		}
	}
	if !sawReturn {
		t.Error("scan never entered the return phase")
	}
	if !done {
		t.Errorf("scan did not complete back to fly-in (phase=%v sub=%d)", m.phase, m.scanSub)
	}
}

// TestFlyInLetters_KeepsMarkHolds verifies the post-chomp replay holds
// the mark (region 0) settled in place while the wordmark letters fly in
// from off-screen right.
func TestFlyInLetters_KeepsMarkHolds(t *testing.T) {
	m := newSetupModel(context.Background(), true, "", func(context.Context, string, string, string) error { return nil })
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(setupModel)
	for i := 0; i < 200; i++ { // settle the initial fly-in
		next, _ = m.Update(logoTickMsg{})
		m = next.(setupModel)
	}
	if len(m.regions) < 2 {
		t.Fatalf("expected mark + letters, got %d regions", len(m.regions))
	}

	m.flyInLetters()

	if m.phase != phaseIntro {
		t.Errorf("phase = %v, want phaseIntro", m.phase)
	}
	// Region 0 (mark) holds its settled spot.
	mark := m.regions[0]
	if mark.Pos != mark.Target {
		t.Errorf("mark Pos=%.1f, want Target=%.1f (mark should not fly)", mark.Pos, mark.Target)
	}
	// Every letter starts off-screen to the right of its target.
	for i := 1; i < len(m.regions); i++ {
		r := m.regions[i]
		if r.Pos <= r.Target {
			t.Errorf("letter %d Pos=%.1f should start right of Target=%.1f (off-screen)", i, r.Pos, r.Target)
		}
	}
}
