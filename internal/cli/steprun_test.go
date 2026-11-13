package cli

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	extend "github.com/extend-hq/extend-go-sdk"
)

// TestStepRunVariantsCovered is the drift guard for the StepRun union
// dispatch in stepRunInfo and stepRunFiles. Both switch on the wire
// stepType, so a variant added by an SDK regen silently renders as a
// blank row and skips its files until it gets a case. For every variant
// field on extend.StepRun, read the discriminator the SDK emits, decode
// a populated step run of that type, and assert both helpers see it.
func TestStepRunVariantsCovered(t *testing.T) {
	typ := reflect.TypeOf(extend.StepRun{})
	checked := 0
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Name == "StepType" || f.PkgPath != "" || f.Type.Kind() != reflect.Ptr {
			continue
		}
		wrapper := reflect.New(typ)
		wrapper.Elem().Field(i).Set(reflect.New(f.Type.Elem()))
		data, err := json.Marshal(wrapper.Interface())
		if err != nil {
			t.Fatalf("marshal StepRun for field %s: %v", f.Name, err)
		}
		var probe struct {
			StepType string `json:"stepType"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			t.Fatalf("unmarshal probe for field %s: %v", f.Name, err)
		}
		if probe.StepType == "" {
			continue
		}
		checked++

		raw := fmt.Sprintf(`{
			"stepType": %q,
			"object": "workflow_step_run",
			"id": "sr_1",
			"workflowRunId": "workflow_run_1",
			"status": "PROCESSED",
			"files": [{"object": "file", "id": "file_1", "name": "a.pdf"}],
			"step": {"object": "workflow_step", "id": "step_1", "name": "my_step", "type": %q}
		}`, probe.StepType, probe.StepType)
		var step extend.StepRun
		if err := json.Unmarshal([]byte(raw), &step); err != nil {
			t.Fatalf("decode %s step run: %v", probe.StepType, err)
		}

		name, stepType, status := stepRunInfo(&step)
		if name != "my_step" || stepType != probe.StepType || status != "PROCESSED" {
			t.Errorf("stepRunInfo(%s) = (%q, %q, %q); add a case for %s", probe.StepType, name, stepType, status, f.Name)
		}
		files := stepRunFiles(&step)
		if len(files) != 1 || files[0].ID != "file_1" {
			t.Errorf("stepRunFiles(%s) = %v; add a case for %s", probe.StepType, files, f.Name)
		}
	}
	if checked < 10 {
		t.Fatalf("only %d step run variants cross-checked; expected the full StepRun set — reflection probe likely broke", checked)
	}
}
