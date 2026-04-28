package output

import (
	"bytes"
	"strings"
	"testing"
)

type fixture struct {
	ID     string         `json:"id"`
	Status string         `json:"status"`
	Output map[string]any `json:"output"`
}

func sample() fixture {
	return fixture{
		ID:     "extract_run_abc",
		Status: "PROCESSED",
		Output: map[string]any{
			"value": map[string]any{"invoice_id": "INV-42", "total": 199.99},
		},
	}
}

func TestParseFormat(t *testing.T) {
	cases := map[string]Format{
		"":      FormatJSON,
		"json":  FormatJSON,
		"yaml":  FormatYAML,
		"yml":   FormatYAML,
		"raw":   FormatRaw,
		"id":    FormatID,
		"table": FormatTable,
	}
	for in, want := range cases {
		got, err := ParseFormat(in)
		if err != nil {
			t.Errorf("ParseFormat(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseFormat(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("ParseFormat(xml) should error")
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatJSON, sample()); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "\"id\": \"extract_run_abc\"") {
		t.Errorf("expected pretty json with id field, got: %s", got)
	}
	if !strings.Contains(got, "\n  \"status\"") {
		t.Errorf("expected indented output, got: %s", got)
	}
}

func TestRenderJSONCompact(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatJSON, sample(), WithPretty(false)); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if strings.Contains(got, "\n") {
		t.Errorf("expected compact json (no newlines), got: %s", got)
	}
}

func TestRenderYAML(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatYAML, sample()); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "id: extract_run_abc") {
		t.Errorf("expected yaml id line, got: %s", got)
	}
}

func TestRenderID(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatID, sample()); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "extract_run_abc" {
		t.Errorf("FormatID = %q, want extract_run_abc", got)
	}
}

func TestRenderIDOnNonObjectErrors(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, FormatID, 42)
	if err == nil {
		t.Error("expected error rendering id on non-object")
	}
}

func TestRenderJQ(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatJSON, sample(), WithJQ(".output.value.invoice_id"), WithPretty(false)); err != nil {
		t.Fatalf("render with jq: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "\"INV-42\"" {
		t.Errorf("jq output = %q, want \"INV-42\"", got)
	}
}

func TestRenderJQRaw(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatRaw, sample(), WithJQ(".output.value.invoice_id")); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "INV-42" {
		t.Errorf("raw jq output = %q, want INV-42", got)
	}
}

func TestRenderJQMultiple(t *testing.T) {
	var buf bytes.Buffer
	payload := []map[string]any{{"id": "a"}, {"id": "b"}}
	if err := Render(&buf, FormatRaw, payload, WithJQ(".[].id")); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	want := "a\nb"
	if got != want {
		t.Errorf("multi-jq = %q, want %q", got, want)
	}
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	err := RenderTable(&buf, []string{"id", "status"}, [][]string{
		{"run_abc", "PROCESSED"},
		{"run_xyz", "FAILED"},
	})
	if err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "ID") || !strings.Contains(got, "STATUS") {
		t.Errorf("expected uppercased headers, got: %s", got)
	}
	if !strings.Contains(got, "run_abc") || !strings.Contains(got, "PROCESSED") {
		t.Errorf("missing row content, got: %s", got)
	}
}

func TestRenderTableRejectedByRender(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, FormatTable, sample())
	if err == nil {
		t.Error("Render(FormatTable, ...) must error; callers should use RenderTable")
	}
}

func TestRenderMarkdownTable(t *testing.T) {
	var buf bytes.Buffer
	err := RenderMarkdownTable(&buf, []string{"id", "status"}, [][]string{
		{"run_abc", "PROCESSED"},
		{"run_xyz", "FAILED"},
	})
	if err != nil {
		t.Fatalf("RenderMarkdownTable: %v", err)
	}
	got := buf.String()
	want := "| ID | STATUS |\n| --- | --- |\n| run_abc | PROCESSED |\n| run_xyz | FAILED |\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderMarkdownTable_EmptyHeadersIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderMarkdownTable(&buf, nil, nil); err != nil {
		t.Fatalf("RenderMarkdownTable: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil headers, got %q", buf.String())
	}
}
