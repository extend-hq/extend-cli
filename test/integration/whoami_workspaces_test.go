package integration

import (
	"strings"
	"testing"
)

// TestWhoami_APIKey checks the fields `extend whoami` reads from GET /me
// for an API key: the workspace, and the key's name and scope.
func TestWhoami_APIKey(t *testing.T) {
	env := requireEnv(t)

	jsonRes := runExtend(t, env, "whoami", "-o", "json")
	jsonRes.requireOK(t, "whoami", "-o", "json")
	var me struct {
		Workspace struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workspace"`
		APIKey *struct {
			Name  string `json:"name"`
			Scope string `json:"scope"`
		} `json:"apiKey"`
	}
	jsonRes.decodeJSON(t, &me)
	if !strings.HasPrefix(me.Workspace.ID, "ws_") {
		t.Errorf("workspace.id = %q, want a ws_ prefix: %s", me.Workspace.ID, jsonRes.Stdout)
	}
	if me.APIKey == nil {
		t.Fatalf("/me has no apiKey block for an API key: %s", jsonRes.Stdout)
	}
	if me.APIKey.Scope != "workspace" && me.APIKey.Scope != "organization" {
		t.Errorf("apiKey.scope = %q, want workspace or organization", me.APIKey.Scope)
	}

	textRes := runExtend(t, env, "whoami")
	textRes.requireOK(t, "whoami")
	text := string(textRes.Stdout)
	for _, want := range []string{"Workspace  " + me.Workspace.Name, "Key        ", "Auth       API key (", "Base URL   "} {
		if !strings.Contains(text, want) {
			t.Errorf("whoami output missing %q:\n%s", want, text)
		}
	}
}

// TestWorkspaces_ListAndGet lists the organization's workspaces and fetches
// the first. `workspaces create` is not covered: the API has no delete, so
// each run would leave a workspace behind.
func TestWorkspaces_ListAndGet(t *testing.T) {
	env := requireEnv(t)

	listRes := runExtend(t, env, "workspaces", "list", "--limit", "5", "-o", "json")
	skipIfUnavailable(t, listRes, "Workspace management", "workspace management (an organization-level API key on an enabled organization)")
	listRes.requireOK(t, "workspaces", "list")
	items := listItems(t, listRes)
	if len(items) == 0 {
		t.Fatalf("workspaces list returned no workspaces; an organization always has one: %s", listRes.Stdout)
	}
	id, _ := items[0]["id"].(string)
	if !strings.HasPrefix(id, "ws_") {
		t.Fatalf("workspace id = %q, want a ws_ prefix", id)
	}

	getRes := runExtend(t, env, "workspaces", "get", id, "-o", "json")
	getRes.requireOK(t, "workspaces", "get", id)
	var got struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	getRes.decodeJSON(t, &got)
	if got.ID != id {
		t.Errorf("get.id = %q, want %q", got.ID, id)
	}
	if want, _ := items[0]["name"].(string); got.Name != want {
		t.Errorf("get.name = %q, want %q", got.Name, want)
	}
}
