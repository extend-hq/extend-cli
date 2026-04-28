package client

import (
	"net/url"
	"strings"
	"testing"
)

// parseQuery extracts a stable map of query params from a path+query string.
// Used in assertions to avoid depending on url.Values's stable ordering.
func parseQuery(t *testing.T, q string) url.Values {
	t.Helper()
	if !strings.HasPrefix(q, "?") {
		t.Fatalf("expected leading '?', got %q", q)
	}
	v, err := url.ParseQuery(q[1:])
	if err != nil {
		t.Fatalf("parse %q: %v", q, err)
	}
	return v
}

func TestListRunsOptionsQuery_Extract(t *testing.T) {
	o := ListRunsOptions{
		Status:           "PROCESSED",
		ProcessorID:      "ex_abc",
		BatchID:          "bpr_x",
		Source:           "API",
		SourceID:         "workflow_run_x",
		FileNameContains: "invoice",
		Limit:            25,
		PageToken:        "tok",
		SortBy:           "updatedAt",
		SortDir:          "asc",
	}
	got := parseQuery(t, o.query(KindExtract))
	wants := map[string]string{
		"status":           "PROCESSED",
		"extractorId":      "ex_abc",
		"batchId":          "bpr_x",
		"source":           "API",
		"sourceId":         "workflow_run_x",
		"fileNameContains": "invoice",
		"maxPageSize":      "25",
		"nextPageToken":    "tok",
		"sortBy":           "updatedAt",
		"sortDir":          "asc",
	}
	for k, v := range wants {
		if got.Get(k) != v {
			t.Errorf("%s = %q, want %q (full: %v)", k, got.Get(k), v, got)
		}
	}
}

func TestListRunsOptionsQuery_Parse_UsesLimitParam_NoSort(t *testing.T) {
	// /parse_runs has its own server schema: paging key is `limit` (NOT
	// `maxPageSize`), and there is no sortBy/sortDir at all. The CLI used
	// to silently send `maxPageSize` here which the server stripped, so
	// `--limit` was a no-op. The kind-aware query() fixes that.
	o := ListRunsOptions{
		Limit:   5,
		SortBy:  "createdAt",
		SortDir: "desc",
		Source:  "API",
	}
	got := parseQuery(t, o.query(KindParse))
	if got.Get("limit") != "5" {
		t.Errorf("expected limit=5, got %q", got.Get("limit"))
	}
	if got.Has("maxPageSize") {
		t.Errorf("parse runs must use 'limit' not 'maxPageSize': %v", got)
	}
	if got.Has("sortBy") || got.Has("sortDir") {
		t.Errorf("parse runs has no sortBy/sortDir per server schema: %v", got)
	}
	if got.Get("source") != "API" {
		t.Errorf("source filter dropped on parse: %v", got)
	}
}

func TestListRunsOptionsQuery_Workflow_NoSourceFilters(t *testing.T) {
	// /workflow_runs schema does not accept source or sourceId.
	o := ListRunsOptions{
		Source:      "API",
		SourceID:    "x",
		ProcessorID: "workflow_abc",
	}
	got := parseQuery(t, o.query(KindWorkflow))
	if got.Has("source") || got.Has("sourceId") {
		t.Errorf("workflow_runs has no source/sourceId per server schema: %v", got)
	}
	if got.Get("workflowId") != "workflow_abc" {
		t.Errorf("expected workflowId=workflow_abc, got %v", got)
	}
}

func TestListRunsOptionsQuery_ProcessorIDParamName(t *testing.T) {
	cases := []struct {
		kind  RunKind
		param string
	}{
		{KindExtract, "extractorId"},
		{KindClassify, "classifierId"},
		{KindSplit, "splitterId"},
		{KindWorkflow, "workflowId"},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			got := parseQuery(t, ListRunsOptions{ProcessorID: "abc"}.query(c.kind))
			if got.Get(c.param) != "abc" {
				t.Errorf("kind=%s expected %s=abc, got %v", c.kind, c.param, got)
			}
		})
	}
}

func TestListRunsOptionsQuery_Parse_NoProcessorID(t *testing.T) {
	// /parse_runs has no processor; ProcessorID must be silently dropped.
	got := ListRunsOptions{ProcessorID: "ex_abc"}.query(KindParse)
	if got != "" {
		v := parseQuery(t, got)
		for _, key := range []string{"extractorId", "classifierId", "splitterId", "workflowId", "processorId"} {
			if v.Has(key) {
				t.Errorf("parse_runs query leaked processor ID under %q: %v", key, v)
			}
		}
	}
}

func TestListFilesOptionsQuery(t *testing.T) {
	o := ListFilesOptions{
		NameContains: "invoice",
		SortDir:      "asc",
		Limit:        50,
		PageToken:    "tok",
	}
	got := parseQuery(t, o.query())
	wants := map[string]string{
		"nameContains":  "invoice",
		"sortDir":       "asc",
		"maxPageSize":   "50",
		"nextPageToken": "tok",
	}
	for k, v := range wants {
		if got.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, got.Get(k), v)
		}
	}
	if got.Has("sortBy") {
		t.Errorf("files endpoint accepts only 'createdAt' for sortBy (default), so the CLI no longer sends it: %v", got)
	}
}

func TestListProcessorsOptionsQuery(t *testing.T) {
	o := ListProcessorsOptions{
		Limit:     10,
		PageToken: "p",
		SortBy:    "updatedAt",
		SortDir:   "asc",
	}
	got := parseQuery(t, o.query())
	wants := map[string]string{
		"maxPageSize":   "10",
		"nextPageToken": "p",
		"sortBy":        "updatedAt",
		"sortDir":       "asc",
	}
	for k, v := range wants {
		if got.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, got.Get(k), v)
		}
	}
}
