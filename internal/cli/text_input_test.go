package cli

import (
	"net/http"
	"strings"
	"testing"
)

// These tests lock in the inline --text input path (FileFromText) for the
// verbs whose SDK file union accepts it: extract, classify, run. The
// plumbing existed in Build*File but nothing populated FileRef.Text, so
// raw-text input was previously unreachable.

func TestExtract_TextInputForwardsText(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract_runs" || r.Method != http.MethodPost {
			t.Fatalf("hit %s %s, want POST /extract_runs", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "exr_x", "object": "extract_run", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "extract")
	cmd.SetArgs([]string{"--using", "ex_abc", "--text", "hello world", "--name", "note.txt", "--wait=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"text":"hello world"`) {
		t.Errorf("body missing inline text under file; got %s", body)
	}
	if !strings.Contains(body, `"name":"note.txt"`) {
		t.Errorf("body missing --name on the text input; got %s", body)
	}
}

func TestExtract_TextAndInputConflict(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("no request expected; got %s %s", r.Method, r.URL.Path)
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "extract")
	cmd.SetArgs([]string{"file_a", "--using", "ex_abc", "--text", "x"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "either an input argument or --text") {
		t.Errorf("expected input/--text conflict error, got %v", err)
	}
}

func TestClassify_TextInputForwardsText(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/classify_runs" || r.Method != http.MethodPost {
			t.Fatalf("hit %s %s, want POST /classify_runs", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "clr_x", "object": "classify_run", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "classify")
	cmd.SetArgs([]string{"--using", "cl_abc", "--text", "some text", "--wait=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"text":"some text"`) {
		t.Errorf("classify body missing inline text; got %s", body)
	}
}

func TestWorkflowRun_TextInputForwardsText(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workflow_runs" || r.Method != http.MethodPost {
			t.Fatalf("hit %s %s, want POST /workflow_runs", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"id": "workflow_run_x", "object": "workflow_run", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "workflows", "run")
	cmd.SetArgs([]string{"--using", "workflow_abc", "--text", "wf text"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"text":"wf text"`) {
		t.Errorf("workflow run body missing inline text; got %s", body)
	}
}
