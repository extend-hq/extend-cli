package cli

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/extend-hq/extend-cli/internal/config"
	"github.com/extend-hq/extend-cli/internal/extendx"
)

// TestResolveCredentials pins the precedence chain that makes `extend
// setup` actually wire every command: flag/env win, the config file is
// the lowest-priority fallback, and an --env label bypasses the file.
func TestResolveCredentials(t *testing.T) {
	file := config.File{Region: "eu", APIKey: "sk_file"}
	loadFile := func() (config.File, error) { return file, nil }
	loadEmpty := func() (config.File, error) { return config.File{}, nil }

	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cases := []struct {
		name       string
		envLabel   string
		regionFlag string
		getenv     func(string) string
		load       func() (config.File, error)
		wantKey    string
		wantRegion string
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
			name:       "env label bypasses config file",
			envLabel:   "test",
			getenv:     env(map[string]string{"EXTEND_TEST_API_KEY": "sk_test"}),
			load:       loadFile, // must be ignored
			wantKey:    "sk_test",
			wantRegion: "", // file region ignored, no env/flag
		},
		{
			name:       "nothing set yields empty key",
			getenv:     env(map[string]string{}),
			load:       loadEmpty,
			wantKey:    "",
			wantRegion: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, region := resolveCredentials(tc.envLabel, tc.regionFlag, tc.getenv, tc.load)
			if key != tc.wantKey {
				t.Errorf("key = %q, want %q", key, tc.wantKey)
			}
			if region != tc.wantRegion {
				t.Errorf("region = %q, want %q", region, tc.wantRegion)
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
	m := newSetupModel(context.Background(), false, "", func(context.Context, string, string) error { return nil })
	if m.step != stepRegion {
		t.Fatalf("initial step = %v, want stepRegion", m.step)
	}
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyDown}) // -> EU
	if m.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", m.cursor)
	}
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyDown}) // clamp at last
	if m.cursor != 1 {
		t.Fatalf("cursor clamped = %d, want 1", m.cursor)
	}
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyEnter})
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
	m := newSetupModel(context.Background(), false, "us", func(context.Context, string, string) error { return nil })
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // pick US
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // submit empty key
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

	m := newSetupModel(context.Background(), false, "eu", func(context.Context, string, string) error { return nil })
	m.region = setupRegionChoices[1] // eu
	m.apiKey = "sk_live_123"
	m.step = stepValidating

	m = drive(t, m, validatedMsg{err: nil})
	if m.step != stepDone {
		t.Fatalf("step = %v, want stepDone", m.step)
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
	if saved.Region != "eu" || saved.APIKey != "sk_live_123" {
		t.Errorf("saved config = %+v, want region=eu key=sk_live_123", saved)
	}
}

// TestSetupModel_ValidateFailureReturnsToKey ensures a rejected key sends
// the user back to the key step with the error shown, not a crash.
func TestSetupModel_ValidateFailureReturnsToKey(t *testing.T) {
	m := newSetupModel(context.Background(), false, "us", func(context.Context, string, string) error { return nil })
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

// TestSetupModel_ValidateCmdInvokesValidator confirms the command the
// model dispatches actually calls the injected validator with the chosen
// region and key.
func TestSetupModel_ValidateCmdInvokesValidator(t *testing.T) {
	var gotRegion, gotKey string
	m := newSetupModel(context.Background(), false, "", func(_ context.Context, region, key string) error {
		gotRegion, gotKey = region, key
		return nil
	})
	m.region = setupRegionChoices[1]
	m.apiKey = "sk_xyz"

	msg := m.validateCmd()()
	if _, ok := msg.(validatedMsg); !ok {
		t.Fatalf("validateCmd produced %T, want validatedMsg", msg)
	}
	if gotRegion != "eu" || gotKey != "sk_xyz" {
		t.Errorf("validator called with (%q,%q), want (eu,sk_xyz)", gotRegion, gotKey)
	}
}

// TestSetupModel_CtrlCCancels ensures ctrl+c flags cancellation from any
// step.
func TestSetupModel_CtrlCCancels(t *testing.T) {
	m := newSetupModel(context.Background(), false, "", func(context.Context, string, string) error { return nil })
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
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
