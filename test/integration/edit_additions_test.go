package integration

import (
	"strings"
	"testing"
	"time"
)

// firstEditTemplate returns the ID of the most recent saved edit template,
// skipping the test when the workspace has none.
func firstEditTemplate(t *testing.T, env envSetup) (id string, item map[string]any) {
	t.Helper()
	listRes := runExtend(t, env, "edit", "templates", "list", "--limit", "5", "-o", "json")
	listRes.requireOK(t, "edit", "templates", "list")
	items := listItems(t, listRes)
	if len(items) == 0 {
		t.Skip("no edit templates in workspace; save one in the dashboard to enable this test")
	}
	id, _ = items[0]["id"].(string)
	if !strings.HasPrefix(id, "edt_") {
		t.Fatalf("template id = %q, want an edt_ prefix", id)
	}
	return id, items[0]
}

func TestEditTemplates_ListAndGet(t *testing.T) {
	env := requireEnv(t)
	id, item := firstEditTemplate(t, env)

	getRes := runExtend(t, env, "edit", "templates", "get", id, "-o", "json")
	getRes.requireOK(t, "edit", "templates", "get", id)
	var got struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	getRes.decodeJSON(t, &got)
	if got.ID != id {
		t.Errorf("get.id = %q, want %q", got.ID, id)
	}
	if want, _ := item["name"].(string); got.Name != want {
		t.Errorf("get.name = %q, want the listed name %q", got.Name, want)
	}
}

func TestEditRuns_List(t *testing.T) {
	env := requireEnv(t)

	res := runExtend(t, env, "edit", "runs", "list", "--limit", "5", "-o", "json")
	res.requireOK(t, "edit", "runs", "list")
	for i, run := range listItems(t, res) {
		if id, _ := run["id"].(string); !strings.HasPrefix(id, "edr_") {
			t.Errorf("item %d id = %q, want an edr_ prefix", i, id)
		}
	}
}

// TestEdit_FillsSavedTemplate starts a fill of a saved template and checks
// the API accepted the template reference and that the run then shows up
// in `edit runs list`. It doesn't wait for the fill to finish.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestEdit_FillsSavedTemplate(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)
	templateID, _ := firstEditTemplate(t, env)

	res := runExtend(t, env,
		"edit", "--template", templateID,
		"--instructions", "Fill every text field with the word TEST.",
		"--wait=false",
		"-o", "json",
	)
	res.requireOK(t, "edit", "--template", templateID)
	var run struct {
		ID string `json:"id"`
	}
	res.decodeJSON(t, &run)
	if !strings.HasPrefix(run.ID, "edr_") {
		t.Fatalf("expected edr_ prefix on run id, got %q", run.ID)
	}
	rememberCleanup(t, env, "delete edit run", "edit", "runs", "delete", run.ID, "-y")

	// A run isn't always in the list the moment it is created.
	start := time.Now()
	for {
		listRes := runExtend(t, env, "edit", "runs", "list", "--sort-by", "createdAt", "--limit", "20", "-o", "json")
		listRes.requireOK(t, "edit", "runs", "list")
		for _, item := range listItems(t, listRes) {
			if item["id"] == run.ID {
				t.Logf("run listed after %s", time.Since(start).Round(100*time.Millisecond))
				return
			}
		}
		if time.Since(start) > 20*time.Second {
			t.Fatalf("edit runs list (20 newest) still doesn't include the new run %s after 20s: %s", run.ID, listRes.Stdout)
		}
		time.Sleep(time.Second)
	}
}
