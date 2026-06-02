package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	extend "github.com/extend-hq/extend-go-sdk"
)

// jsonFieldNames returns the top-level JSON field names declared on a
// struct type via its `json:"..."` tags, skipping unexported fields and
// the "-" sentinel. Used to assert the hand-written config catalogs in
// configdocs.go stay in sync with the SDK config structs.
func jsonFieldNames(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported (e.g. extraProperties)
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// TestConfigCatalogsCoverSDKFields is the drift guard: the catalogs in
// configdocs.go are hand-written (so they can carry descriptions, enum
// values, and required-ness that runtime reflection can't recover), but
// every top-level field on the corresponding SDK config struct must be
// named somewhere in the catalog. When the pinned SDK gains a config
// field, this fails until the catalog documents it.
func TestConfigCatalogsCoverSDKFields(t *testing.T) {
	cases := []struct {
		name    string
		typ     reflect.Type
		catalog string
	}{
		{"SplitConfig", reflect.TypeOf(extend.SplitConfig{}), splitConfigFields},
		{"ExtractConfigJSON", reflect.TypeOf(extend.ExtractConfigJSON{}), extractConfigFields},
		{"ClassifyConfig", reflect.TypeOf(extend.ClassifyConfig{}), classifyConfigFields},
		// Parse --block-options: top-level blocks plus every nested block
		// struct's leaves, all named in parseBlockOptionsFields.
		{"ParseConfigBlockOptions", reflect.TypeOf(extend.ParseConfigBlockOptions{}), parseBlockOptionsFields},
		{"ParseConfigBlockOptionsFigures", reflect.TypeOf(extend.ParseConfigBlockOptionsFigures{}), parseBlockOptionsFields},
		{"ParseConfigBlockOptionsTables", reflect.TypeOf(extend.ParseConfigBlockOptionsTables{}), parseBlockOptionsFields},
		{"ParseConfigBlockOptionsTablesAgentic", reflect.TypeOf(extend.ParseConfigBlockOptionsTablesAgentic{}), parseBlockOptionsFields},
		{"ParseConfigBlockOptionsText", reflect.TypeOf(extend.ParseConfigBlockOptionsText{}), parseBlockOptionsFields},
		{"ParseConfigBlockOptionsTextAgentic", reflect.TypeOf(extend.ParseConfigBlockOptionsTextAgentic{}), parseBlockOptionsFields},
		{"ParseConfigBlockOptionsKeyValue", reflect.TypeOf(extend.ParseConfigBlockOptionsKeyValue{}), parseBlockOptionsFields},
		{"ParseConfigBlockOptionsBarcodes", reflect.TypeOf(extend.ParseConfigBlockOptionsBarcodes{}), parseBlockOptionsFields},
		{"ParseConfigBlockOptionsFormulas", reflect.TypeOf(extend.ParseConfigBlockOptionsFormulas{}), parseBlockOptionsFields},
		// Parse --advanced-options: top-level plus the returnOcr leaf.
		{"ParseConfigAdvancedOptions", reflect.TypeOf(extend.ParseConfigAdvancedOptions{}), parseAdvancedOptionsFields},
		{"ParseConfigAdvancedOptionsReturnOcr", reflect.TypeOf(extend.ParseConfigAdvancedOptionsReturnOcr{}), parseAdvancedOptionsFields},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range jsonFieldNames(tc.typ) {
				if !strings.Contains(tc.catalog, field) {
					t.Errorf("%s catalog is missing SDK config field %q — document it in configdocs.go", tc.name, field)
				}
			}
		})
	}
}

// TestWorkflowStepTypesCoverSDK is the non-circular guard for the workflow
// step-type catalog. Rather than checking that the help contains strings we
// wrote (which would be circular), it asks the SDK itself: for every step
// variant on WorkflowStepDefinition, instantiate it, marshal the wrapper,
// and read back the "type" discriminator the SDK emits — then assert that
// exact wire value appears in workflowStepsFields. Reflecting over the
// struct's fields means a new step type added by an SDK regen is covered
// automatically.
func TestWorkflowStepTypesCoverSDK(t *testing.T) {
	typ := reflect.TypeOf(extend.WorkflowStepDefinition{})
	checked := 0
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		// Skip the bare discriminator field and any unexported field;
		// the variant payloads are the exported pointer-to-struct fields.
		if f.Name == "Type" || f.PkgPath != "" || f.Type.Kind() != reflect.Ptr {
			continue
		}
		wrapper := reflect.New(typ)
		wrapper.Elem().Field(i).Set(reflect.New(f.Type.Elem()))
		data, err := json.Marshal(wrapper.Interface())
		if err != nil {
			t.Fatalf("marshal WorkflowStepDefinition for field %s: %v", f.Name, err)
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			t.Fatalf("unmarshal probe for field %s: %v", f.Name, err)
		}
		if probe.Type == "" {
			continue // not a discriminated step variant
		}
		checked++
		if !strings.Contains(workflowStepsFields, probe.Type) {
			t.Errorf("workflowStepsFields is missing SDK step type %q (from field %s) — document it in configdocs.go", probe.Type, f.Name)
		}
	}
	if checked < 10 {
		t.Fatalf("only %d step types cross-checked; expected the full WorkflowStepDefinition set — reflection probe likely broke", checked)
	}
}

// TestProcessorCreateHelpDocumentsConfig asserts the rendered create-help
// for each processor family enumerates its config shape (rather than
// punting to "copy an existing one") and points at the draft as the clone
// source. Guards the wiring of bodyDoc into the generic create doc.
func TestProcessorCreateHelpDocumentsConfig(t *testing.T) {
	ta := newTestApp(t, newFakeServer(t, nil))
	cases := []struct {
		plural string
		tokens []string
	}{
		{"splitters", []string{"splitClassifications", "splitting_performance", "splitRules"}},
		{"extractors", []string{"schema", "extraction_performance", "extractionRules"}},
		{"classifiers", []string{"classifications", "classification_performance"}},
		{"workflows", []string{"steps", "FILE_CONVERSION", "CONDITIONAL_EXTRACT"}},
	}
	for _, tc := range cases {
		t.Run(tc.plural, func(t *testing.T) {
			cmd := findCmd(t, ta.app, tc.plural, "create")
			long := cmd.Long
			for _, want := range tc.tokens {
				if !strings.Contains(long, want) {
					t.Errorf("%s create --help missing %q in:\n%s", tc.plural, want, long)
				}
			}
			// The misleading "1.0"-only clone hint must now offer draft.
			if !strings.Contains(long, "draft") {
				t.Errorf("%s create --help should suggest 'draft' as the clone source", tc.plural)
			}
		})
	}
}

// TestActionVerbConfigHelpDocumentsFields asserts the inline --config
// field catalog reaches the action verbs (extract/classify/split), so an
// agent building a one-off config can discover the shape from --help.
func TestActionVerbConfigHelpDocumentsFields(t *testing.T) {
	ta := newTestApp(t, newFakeServer(t, nil))
	cases := []struct {
		verb   string
		tokens []string
	}{
		{"extract", []string{"schema", "extraction_performance"}},
		{"classify", []string{"classifications", "classification_performance"}},
		{"split", []string{"splitClassifications", "splitting_performance"}},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			cmd := findCmd(t, ta.app, tc.verb)
			for _, want := range tc.tokens {
				if !strings.Contains(cmd.Long, want) {
					t.Errorf("%s --help missing --config field %q", tc.verb, want)
				}
			}
		})
	}
}

// TestEditSchemaPropsCoverSDK is the drift guard for the edit schema
// property catalog: every extend_edit:* JSON tag on the SDK's EditJSON
// must be named in editSchemaPropertyDoc. Reflecting over the tags means
// a new annotation added by an SDK regen fails until it's documented.
func TestEditSchemaPropsCoverSDK(t *testing.T) {
	checked := 0
	for _, name := range jsonFieldNames(reflect.TypeOf(extend.EditJSON{})) {
		if !strings.HasPrefix(name, "extend_edit:") {
			continue
		}
		checked++
		if !strings.Contains(editSchemaPropertyDoc, name) {
			t.Errorf("editSchemaPropertyDoc is missing EditJSON annotation %q — document it in configdocs.go", name)
		}
	}
	if checked < 9 {
		t.Fatalf("only %d extend_edit:* keys cross-checked; expected >=9 — reflection probe likely broke", checked)
	}
}

// TestParseHelpDocumentsOptions asserts the block/advanced field catalogs
// reach `extend parse --help`, so an agent building --block-options or
// --advanced-options JSON can discover the shapes.
func TestParseHelpDocumentsOptions(t *testing.T) {
	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "parse")
	for _, want := range []string{
		"advancedChartExtractionEnabled", // block: figures
		"targetFormat",                   // block: tables
		"signatureDetectionEnabled",      // block: text
		"verticalGroupingThreshold",      // advanced
		"formattingDetection",            // advanced
	} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("parse --help missing option field %q", want)
		}
	}
}

// TestEditSchemaGenerateHelpDocumentsProps asserts the extend_edit:* key
// reference reaches `extend edit schema generate --help`.
func TestEditSchemaGenerateHelpDocumentsProps(t *testing.T) {
	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "edit", "schema", "generate")
	for _, want := range []string{"extend_edit:value", "extend_edit:field_type", "extend_edit:bbox"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("edit schema generate --help missing %q", want)
		}
	}
}
