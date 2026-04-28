package cli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/extend-hq/extend-cli/internal/client"
)

func TestMergeBody_FromFileOnlyPreservesBytes(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "body.json")
	original := []byte(`{"name":"a","schema":{"type":"object"},"big":12345678901234567890}`)
	if err := os.WriteFile(tmp, original, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := mergeBody(tmp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("expected raw bytes preserved when no overrides;\n got %s\nwant %s", got, original)
	}
}

func TestMergeBody_InvalidJSONFromFileOnlyErrors(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(tmp, []byte(`not json {`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeBody(tmp, nil); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestMergeBody_OverridesWinOverFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(tmp, []byte(`{"name":"file-name","description":"file-desc"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := mergeBody(tmp, map[string]string{"name": "flag-name"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(got, &m)
	if m["name"] != "flag-name" {
		t.Errorf("name = %v, want flag-name", m["name"])
	}
	if m["description"] != "file-desc" {
		t.Errorf("description = %v, want file-desc (preserved)", m["description"])
	}
}

func TestMergeBody_FromInlineJSON(t *testing.T) {
	got, err := mergeBody(`{"name":"inline","description":"ok"}`, map[string]string{"description": "override"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(got, &m)
	if m["name"] != "inline" || m["description"] != "override" {
		t.Errorf("unexpected merged body: %s", got)
	}
}

func TestMergeBody_FromFileURI(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "body with spaces.json")
	if err := os.WriteFile(tmp, []byte(`{"name":"from-uri"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := mergeBody((&url.URL{Scheme: "file", Path: tmp}).String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"name":"from-uri"}` {
		t.Errorf("got %s", got)
	}
}

func TestMergeBody_NoFileBuildsFromOverrides(t *testing.T) {
	got, err := mergeBody("", map[string]string{"name": "only-from-flag"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(got, &m)
	if m["name"] != "only-from-flag" {
		t.Errorf("name = %v", m["name"])
	}
}

func TestMergeBody_EmptyOverridesIgnored(t *testing.T) {
	got, err := mergeBody("", map[string]string{"name": "", "description": ""})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{}" {
		t.Errorf("expected empty json object, got %s", got)
	}
}

func TestMergeBody_InvalidJSONErrors(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(tmp, []byte(`not json {`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := mergeBody(tmp, nil)
	if err == nil || !strings.Contains(err.Error(), "--from-file") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestArticleFor(t *testing.T) {
	correct := map[string]string{
		"extractor":  "an",
		"evaluator":  "an",
		"item":       "an",
		"order":      "an",
		"upload":     "an",
		"classifier": "a",
		"splitter":   "a",
		"workflow":   "a",
		"":           "a",
	}
	for noun, want := range correct {
		if got := articleFor(noun); got != want {
			t.Errorf("articleFor(%q) = %q, want %q", noun, got, want)
		}
	}

	knownLimitations := []string{"honor", "hour", "X-ray", "MBA"}
	for _, noun := range knownLimitations {
		_ = articleFor(noun)
	}
}

func TestPluralize(t *testing.T) {
	cases := map[int]string{0: "s", 1: "", 2: "s", 100: "s"}
	for n, want := range cases {
		if got := pluralize(n); got != want {
			t.Errorf("pluralize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestMetaFlags_Build(t *testing.T) {
	tests := []struct {
		name     string
		metadata []string
		tags     []string
		wantErr  bool
		check    func(t *testing.T, m map[string]any)
	}{
		{
			name:     "metadata only",
			metadata: []string{"customer=acme", "env=prod"},
			check: func(t *testing.T, m map[string]any) {
				if m["customer"] != "acme" || m["env"] != "prod" {
					t.Errorf("got %+v", m)
				}
			},
		},
		{
			name: "tags only",
			tags: []string{"a,b", "c"},
			check: func(t *testing.T, m map[string]any) {
				tags, ok := m[usageTagsKey].([]string)
				if !ok || len(tags) != 3 || tags[0] != "a" || tags[1] != "b" || tags[2] != "c" {
					t.Errorf("got %+v", m)
				}
			},
		},
		{
			name:     "metadata invalid format",
			metadata: []string{"no-equals"},
			wantErr:  true,
		},
		{
			name:     "metadata empty key",
			metadata: []string{"=value"},
			wantErr:  true,
		},
		{
			name: "empty inputs return nil",
			check: func(t *testing.T, m map[string]any) {
				if m != nil {
					t.Errorf("expected nil, got %+v", m)
				}
			},
		},
		{
			name:     "metadata with equals in value",
			metadata: []string{"key=a=b=c"},
			check: func(t *testing.T, m map[string]any) {
				if m["key"] != "a=b=c" {
					t.Errorf("got %+v, want value 'a=b=c'", m)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mf := metaFlags{metadata: tc.metadata, tags: tc.tags}
			got, err := mf.build()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			tc.check(t, got)
		})
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"a,b,c"}, []string{"a", "b", "c"}},
		{[]string{"a", "b"}, []string{"a", "b"}},
		{[]string{"a,b", "c"}, []string{"a", "b", "c"}},
		{[]string{"  a , b  ,c"}, []string{"a", "b", "c"}},
		{[]string{",,a,,"}, []string{"a"}},
		{nil, nil},
	}
	for _, tc := range tests {
		got := splitCSV(tc.in)
		if !equalStrings(got, tc.want) {
			t.Errorf("splitCSV(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestStatusIcon_PerStatus(t *testing.T) {
	cases := []struct {
		s        client.RunStatus
		wantRune string
	}{
		{client.StatusProcessed, "✓"},
		{client.StatusFailed, "✗"},
		{client.StatusCancelled, "○"},
		{client.StatusCancelling, "○"},
		{client.StatusRejected, "✗"},
		{client.StatusNeedsReview, "⏸"},
		{client.StatusPending, "⋯"},
		{client.StatusProcessing, "⋯"},
		{client.RunStatus("UNKNOWN"), "•"},
	}
	colorless := palette{enabled: false}
	colored := palette{enabled: true}
	for _, tc := range cases {
		if got := statusIcon(colorless, tc.s); got != tc.wantRune {
			t.Errorf("colorless status=%s = %q, want %q", tc.s, got, tc.wantRune)
		}
		got := statusIcon(colored, tc.s)
		if !strings.Contains(got, tc.wantRune) {
			t.Errorf("colored status=%s = %q, missing rune %q", tc.s, got, tc.wantRune)
		}
	}
}

func TestOutputFileID_FromEditedFile(t *testing.T) {
	r := &client.EditRun{Output: &client.EditOutput{EditedFile: &client.EditedFile{ID: "file_filled", PresignedURL: "https://x.com/dl"}}}
	if got := outputFileID(r); got != "file_filled" {
		t.Errorf("got %q, want file_filled", got)
	}
}

func TestOutputFileID_NoOutputReturnsEmpty(t *testing.T) {
	r := &client.EditRun{}
	if got := outputFileID(r); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
