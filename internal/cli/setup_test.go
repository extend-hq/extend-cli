package cli

import (
	"context"
	"errors"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/extend-hq/extend-cli/internal/config"
	"github.com/extend-hq/extend-cli/internal/extendx"
)

// TestResolveCredentials pins the precedence chain that makes `extend
// setup` actually wire every command: flag/env win, the config file is
// the lowest-priority fallback, and an --env label scopes only the API
// key (the file's region still applies under any label).
func TestResolveCredentials(t *testing.T) {
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveCredentials(tc.envLabel, tc.regionFlag, tc.workspaceFlag, tc.getenv, tc.load)
			if got.key != tc.wantKey {
				t.Errorf("key = %q, want %q", got.key, tc.wantKey)
			}
			if got.region != tc.wantRegion {
				t.Errorf("region = %q, want %q", got.region, tc.wantRegion)
			}
			if got.baseURL != tc.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", got.baseURL, tc.wantBaseURL)
			}
			if got.workspaceID != tc.wantWorkspaceID {
				t.Errorf("workspaceID = %q, want %q", got.workspaceID, tc.wantWorkspaceID)
			}
		})
	}
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
	if got := setupValidationMessage(errEmptyKey); got != "Please paste an API key." {
		t.Errorf("empty-key message = %q", got)
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

// TestSetupModel_EmptyKeyRejected ensures submitting a blank key shows an
// inline error and stays on the key step.
func TestSetupModel_EmptyKeyRejected(t *testing.T) {
	m := newSetupModel(context.Background(), false, "us", func(context.Context, string, string, string) error { return nil })
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // pick US
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // submit empty key
	if m.step != stepKey {
		t.Fatalf("step = %v, want stepKey (empty key should not advance)", m.step)
	}
	if !errors.Is(m.valErr, errEmptyKey) {
		t.Errorf("valErr = %v, want errEmptyKey", m.valErr)
	}
}

// TestSetupModel_ValidateSuccessSaves drives the model to a successful
// validation and asserts the config file is written with the chosen
// region and key.
func TestSetupModel_ValidateSuccessSaves(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	m := newSetupModel(context.Background(), false, "eu", func(context.Context, string, string, string) error { return nil })
	m.region = setupRegionChoices[1] // eu
	m.apiKey = "sk_live_123"
	m.step = stepValidating

	m = drive(t, m, validatedMsg{err: nil})
	// On success the config is saved immediately and the wizard advances
	// to the skill-install prompt (not straight to done).
	if m.step != stepSkill {
		t.Fatalf("step = %v, want stepSkill", m.step)
	}
	if m.result == nil || m.result.saveErr != nil {
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
