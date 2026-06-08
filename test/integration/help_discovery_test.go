package integration

import (
	"strings"
	"testing"
)

// These tests close the P1 discoverability gaps from the docs→CLI audit by
// asserting the shipped binary names the JSON knobs and event types in
// --help, so an agent can discover them without reading the raw API
// reference. Pure --help inspection: no API, no credits.

// TestRegionHelp_HidesDeprecatedUS2 guards the "accept but don't
// advertise" stance: --help and the auth topic present only us|eu, never
// the legacy us2 region (which still resolves via --region /
// EXTEND_REGION — that acceptance is covered by extendx unit tests).
func TestRegionHelp_HidesDeprecatedUS2(t *testing.T) {
	root := runExtend(t, envSetup{}, "--help")
	root.requireOK(t, "--help")
	if !strings.Contains(string(root.Stdout), "us|eu") {
		t.Errorf("root --help should advertise region as us|eu; got:\n%s", root.Stdout)
	}
	if strings.Contains(string(root.Stdout), "us2") {
		t.Errorf("root --help must not advertise us2; got:\n%s", root.Stdout)
	}

	auth := runExtend(t, envSetup{}, "help", "auth")
	auth.requireOK(t, "help", "auth")
	if got := string(auth.Stdout) + string(auth.Stderr); strings.Contains(got, "us2") {
		t.Errorf("help auth must not surface us2; got:\n%s", got)
	}
}

// TestParseOptions_ExposedInBinary asserts `extend parse --help` enumerates
// the --block-options and --advanced-options sub-knobs (previously only the
// flag categories were shown).
func TestParseOptions_ExposedInBinary(t *testing.T) {
	res := runExtend(t, envSetup{}, "parse", "--help")
	res.requireOK(t, "parse", "--help")
	for _, want := range []string{
		"--block-options",
		"advancedChartExtractionEnabled",
		"targetFormat",
		"--advanced-options",
		"verticalGroupingThreshold",
		"formattingDetection",
	} {
		if !strings.Contains(string(res.Stdout), want) {
			t.Errorf("parse --help missing %q; got:\n%s", want, res.Stdout)
		}
	}
}

// TestWebhookEvents_ExposedInBinary asserts the valid --events values are
// enumerated in the endpoint create help, across event families.
func TestWebhookEvents_ExposedInBinary(t *testing.T) {
	res := runExtend(t, envSetup{}, "webhooks", "endpoints", "create", "--help")
	res.requireOK(t, "webhooks", "endpoints", "create", "--help")
	for _, want := range []string{
		"extract_run.processed",
		"workflow_run.completed",
		"extractor.version.published",
		"workflow.deployed",
	} {
		if !strings.Contains(string(res.Stdout), want) {
			t.Errorf("webhooks endpoints create --help missing event %q; got:\n%s", want, res.Stdout)
		}
	}
}

// TestEditTemplatesGet_ExposedInBinary asserts the new read-only command
// is wired into the shipped binary and documents the edt_ template ID.
func TestEditTemplatesGet_ExposedInBinary(t *testing.T) {
	res := runExtend(t, envSetup{}, "edit", "templates", "get", "--help")
	res.requireOK(t, "edit", "templates", "get", "--help")
	for _, want := range []string{"edit template", "edt_"} {
		if !strings.Contains(string(res.Stdout), want) {
			t.Errorf("edit templates get --help missing %q; got:\n%s", want, res.Stdout)
		}
	}
}

// TestEditSchemaProps_ExposedInBinary asserts the extend_edit:* key
// reference is documented in the edit schema generate help.
func TestEditSchemaProps_ExposedInBinary(t *testing.T) {
	res := runExtend(t, envSetup{}, "edit", "schema", "generate", "--help")
	res.requireOK(t, "edit", "schema", "generate", "--help")
	for _, want := range []string{"extend_edit:value", "extend_edit:field_type", "extend_edit:bbox"} {
		if !strings.Contains(string(res.Stdout), want) {
			t.Errorf("edit schema generate --help missing %q; got:\n%s", want, res.Stdout)
		}
	}
}
