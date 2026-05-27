package main

import (
	"reflect"
	"testing"

	"github.com/extend-hq/extend-cli/evals/runner/harness"
)

func TestHarnessFamily(t *testing.T) {
	cases := map[string]string{
		"claude_code":                   "claude_code",
		"claude_code:claude-sonnet-4-6": "claude_code",
		"claude_code:claude-opus-4-7":   "claude_code",
		"codex":                         "codex",
	}
	for in, want := range cases {
		if got := harnessFamily(in); got != want {
			t.Errorf("harnessFamily(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestSplitCommaList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,, c ", []string{"a", "b", "c"}}, // trims + drops empties
	}
	for _, tc := range cases {
		if got := splitCommaList(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitCommaList(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestClaudeName_ModelPinning(t *testing.T) {
	if got := (&harness.Claude{}).Name(); got != "claude_code" {
		t.Errorf("unpinned Claude.Name() = %q; want claude_code", got)
	}
	if got := (&harness.Claude{Model: "claude-sonnet-4-6"}).Name(); got != "claude_code:claude-sonnet-4-6" {
		t.Errorf("pinned Claude.Name() = %q; want claude_code:claude-sonnet-4-6", got)
	}
}

// TestPathSafe locks the on-disk sanitization so model-pinned harness
// names (which contain ':') don't produce surprising directory names
// or break CI artifact upload on case-insensitive / colon-hostile FSes.
func TestPathSafe(t *testing.T) {
	cases := map[string]string{
		"claude_code":                   "claude_code",
		"claude_code:claude-sonnet-4-6": "claude_code_claude-sonnet-4-6",
		"codex":                         "codex",
	}
	for in, want := range cases {
		if got := pathSafe(in); got != want {
			t.Errorf("pathSafe(%q) = %q; want %q", in, got, want)
		}
	}
}

// TestClaudeModelCandidates verifies the candidate-expansion logic that
// pickHarnesses uses: N models -> N pinned Claude drivers + Codex; no
// models -> one unpinned Claude + Codex. We replicate the expansion
// here (rather than call pickHarnesses) because pickHarnesses also runs
// Available() PATH checks that aren't present in unit-test CI.
func TestClaudeModelCandidates(t *testing.T) {
	expand := func(models string) []string {
		var names []string
		if ms := splitCommaList(models); len(ms) > 0 {
			for _, m := range ms {
				names = append(names, (&harness.Claude{Model: m}).Name())
			}
		} else {
			names = append(names, (&harness.Claude{}).Name())
		}
		names = append(names, (&harness.Codex{}).Name())
		return names
	}

	if got := expand(""); !reflect.DeepEqual(got, []string{"claude_code", "codex"}) {
		t.Errorf("no models -> %v; want [claude_code codex]", got)
	}
	got := expand("claude-opus-4-7,claude-sonnet-4-6")
	want := []string{"claude_code:claude-opus-4-7", "claude_code:claude-sonnet-4-6", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("two models -> %v; want %v", got, want)
	}
}
