package integration

import (
	"os"
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

// requireOrgKey returns the connection details for the organization-level
// API key in EXTEND_ORG_API_KEY and the ID and name of a workspace in its
// organization, skipping the test when the key isn't set. An
// organization-level key must name a workspace on every request; the
// regular key's workspace is in the same organization.
func requireOrgKey(t *testing.T) (orgEnv envSetup, workspaceID, workspaceName string) {
	t.Helper()
	env := requireEnv(t)
	orgKey := os.Getenv("EXTEND_ORG_API_KEY")
	if orgKey == "" {
		t.Skip("EXTEND_ORG_API_KEY not set; skipping test that needs an organization-level API key")
	}
	res := runExtend(t, env, "whoami", "-o", "json")
	res.requireOK(t, "whoami", "-o", "json")
	var me struct {
		Workspace struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workspace"`
	}
	res.decodeJSON(t, &me)
	return envSetup{BaseURL: env.BaseURL, APIKey: orgKey}, me.Workspace.ID, me.Workspace.Name
}

// TestWhoami_OrganizationKey checks that an organization-level key is
// refused without a workspace and identified with one.
func TestWhoami_OrganizationKey(t *testing.T) {
	orgEnv, workspaceID, _ := requireOrgKey(t)

	bare := runExtend(t, orgEnv, "whoami")
	if bare.ExitCode == 0 {
		t.Fatalf("whoami with an organization-level key and no workspace succeeded: %s", bare.Stdout)
	}
	if !strings.Contains(string(bare.Stderr), "X-Extend-Workspace-Id header is required") {
		t.Errorf("stderr doesn't say a workspace is required: %s", bare.Stderr)
	}

	res := runExtend(t, orgEnv, "whoami", "--workspace", workspaceID, "-o", "json")
	res.requireOK(t, "whoami", "--workspace", workspaceID)
	var me struct {
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
		APIKey struct {
			Scope string `json:"scope"`
		} `json:"apiKey"`
	}
	res.decodeJSON(t, &me)
	if me.Workspace.ID != workspaceID {
		t.Errorf("workspace.id = %q, want %q", me.Workspace.ID, workspaceID)
	}
	if me.APIKey.Scope != "organization" {
		t.Errorf("apiKey.scope = %q, want organization", me.APIKey.Scope)
	}
}

// TestWorkspaces_ListAndGet lists the organization's workspaces and fetches
// one. `workspaces create` is not covered: the API has no delete, so each
// run would leave a workspace behind.
func TestWorkspaces_ListAndGet(t *testing.T) {
	orgEnv, workspaceID, workspaceName := requireOrgKey(t)

	listRes := runExtend(t, orgEnv, "workspaces", "list", "--workspace", workspaceID, "--limit", "5", "-o", "json")
	listRes.requireOK(t, "workspaces", "list")
	items := listItems(t, listRes)
	if len(items) == 0 {
		t.Fatalf("workspaces list returned no workspaces; an organization always has one: %s", listRes.Stdout)
	}
	for i, item := range items {
		if id, _ := item["id"].(string); !strings.HasPrefix(id, "ws_") {
			t.Errorf("item %d id = %q, want a ws_ prefix", i, id)
		}
	}

	getRes := runExtend(t, orgEnv, "workspaces", "get", workspaceID, "--workspace", workspaceID, "-o", "json")
	getRes.requireOK(t, "workspaces", "get", workspaceID)
	var got struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	getRes.decodeJSON(t, &got)
	if got.ID != workspaceID {
		t.Errorf("get.id = %q, want %q", got.ID, workspaceID)
	}
	if got.Name != workspaceName {
		t.Errorf("get.name = %q, want %q", got.Name, workspaceName)
	}
}
