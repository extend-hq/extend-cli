package cli

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

// The CLI decodes JSON flags into SDK request types and encodes them again.
// These cases pin JSON the SDK used to drop or rewrite on that round trip:
// each one must reach the server with the given value at the given path.
func TestJSONFlagsReachTheServerIntact(t *testing.T) {
	cases := []struct {
		name string
		args []string
		path []any
		want any
	}{
		{
			name: "classifier evaluation item",
			args: []string{"evaluations", "items", "create", "ev_1", "--from-file",
				`{"items":[{"fileId":"file_1","expectedOutput":{"id":"cls_a","type":"invoice"}}]}`},
			path: []any{"items", 0, "expectedOutput"},
			want: map[string]any{"id": "cls_a", "type": "invoice"},
		},
		{
			name: "splitter evaluation item",
			args: []string{"evaluations", "items", "create", "ev_1", "--from-file",
				`{"items":[{"fileId":"file_1","expectedOutput":{"splits":[{"classificationId":"c1","type":"invoice","startPage":1,"endPage":2}]}}]}`},
			path: []any{"items", 0, "expectedOutput", "splits", 0, "classificationId"},
			want: "c1",
		},
		{
			name: "multi-file evaluation item",
			args: []string{"evaluations", "items", "create", "ev_1", "--from-file",
				`{"items":[{"fileIds":["file_1","file_2"],"expectedOutput":{"value":{"total":1}}}]}`},
			path: []any{"items", 0, "fileIds"},
			want: []any{"file_1", "file_2"},
		},
		{
			name: "classifier output seeded into a workflow run",
			args: []string{"workflows", "run", "file_1", "--using", "workflow_1", "--outputs",
				`[{"processorId":"cl_1","output":{"id":"cls_a","type":"invoice"}}]`},
			path: []any{"outputs", 0, "output"},
			want: map[string]any{"id": "cls_a", "type": "invoice"},
		},
		{
			name: "extractor generated from a file ID",
			args: []string{"extractors", "create", "--from-file",
				`{"name":"Invoices","generate":{"files":[{"id":"file_1"}]}}`},
			path: []any{"generate", "files", 0},
			want: map[string]any{"id": "file_1"},
		},
		{
			name: "structured key-value output",
			args: []string{"parse", "file_1", "--wait=false", "--block-options",
				`{"keyValue":{"structuredOutputEnabled":true}}`},
			path: []any{"config", "blockOptions", "keyValue", "structuredOutputEnabled"},
			want: true,
		},
		{
			name: "line-level OCR",
			args: []string{"parse", "file_1", "--wait=false", "--advanced-options",
				`{"returnOcr":{"lines":true}}`},
			path: []any{"config", "advancedOptions", "returnOcr", "lines"},
			want: true,
		},
		{
			name: "auto parse engine",
			args: []string{"parse", "file_1", "--wait=false", "--engine", "parse_auto"},
			path: []any{"config", "engine"},
			want: "parse_auto",
		},
		{
			name: "split strict identifier mode",
			args: []string{"split", "file_1", "--wait=false", "--config",
				`{"splitClassifications":[{"id":"o","type":"other","description":"d"}],"advancedOptions":{"strictIdentifierModeEnabled":true}}`},
			path: []any{"config", "advancedOptions", "strictIdentifierModeEnabled"},
			want: true,
		},
		{
			name: "edit signature and schema refinement options",
			args: []string{"edit", "file_1", "--wait=false", "--advanced-options",
				`{"preserveSignatureValidity":true,"advancedSchemaRefinementEnabled":true}`},
			path: []any{"config", "advancedOptions"},
			want: map[string]any{"preserveSignatureValidity": true, "advancedSchemaRefinementEnabled": true},
		},
		{
			name: "edit schema with native form field details",
			args: []string{"edit", "file_1", "--wait=false", "--schema",
				`{"type":"object","properties":{"marital":{"type":["string","null"],"enum":["s","m"],"extend:descriptions":["Single","Married"],"extend_edit:source_acroform":{"fieldName":"Marital1","fieldType":"radio"}}}}`},
			path: []any{"config", "schema", "properties", "marital"},
			want: map[string]any{
				"type":                        []any{"string", "null"},
				"enum":                        []any{"s", "m"},
				"extend:descriptions":         []any{"Single", "Married"},
				"extend_edit:source_acroform": map[string]any{"fieldName": "Marital1", "fieldType": "radio"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST", "message": "stop here"})
			})
			ta := newTestApp(t, srv)
			cmd := findCmd(t, ta.app, tc.args[0])
			cmd.SetArgs(tc.args[1:])
			cmd.SetOut(ta.out)
			cmd.SetErr(ta.errOut)
			_ = cmd.Execute()

			var body any
			if err := json.Unmarshal(srv.lastRequest().Body, &body); err != nil {
				t.Fatalf("request body is not JSON: %v\n%s", err, srv.lastRequest().Body)
			}
			got := body
			for _, step := range tc.path {
				switch key := step.(type) {
				case string:
					m, ok := got.(map[string]any)
					if !ok {
						t.Fatalf("no %q in request body:\n%s", key, srv.lastRequest().Body)
					}
					got = m[key]
				case int:
					a, ok := got.([]any)
					if !ok || key >= len(a) {
						t.Fatalf("no [%d] in request body:\n%s", key, srv.lastRequest().Body)
					}
					got = a[key]
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("sent %v at %v, want %v\nfull body: %s", got, tc.path, tc.want, srv.lastRequest().Body)
			}
		})
	}
}
