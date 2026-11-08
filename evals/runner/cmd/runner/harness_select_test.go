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
		"codex:gpt-6.1-sol":             "codex",
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

// TestHarnessCandidates covers the expansion pickHarnesses filters
// (pickHarnesses itself also checks PATH): each pinned model becomes its
// own harness, and a family with no models runs once on its default.
func TestHarnessCandidates(t *testing.T) {
	names := func(claudeModels, codexModels string) []string {
		var out []string
		for _, d := range harnessCandidates(claudeModels, codexModels) {
			out = append(out, d.Name())
		}
		return out
	}

	if got, want := names("", ""), []string{"claude_code", "codex"}; !reflect.DeepEqual(got, want) {
		t.Errorf("no models -> %v; want %v", got, want)
	}
	got := names("claude-opus-5-5,claude-sonnet-5-5", "")
	want := []string{"claude_code:claude-opus-5-5", "claude_code:claude-sonnet-5-5", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("two Claude models -> %v; want %v", got, want)
	}
	got = names("", "gpt-6.1-sol,gpt-5.5")
	want = []string{"claude_code", "codex:gpt-6.1-sol", "codex:gpt-5.5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("two Codex models -> %v; want %v", got, want)
	}
}
