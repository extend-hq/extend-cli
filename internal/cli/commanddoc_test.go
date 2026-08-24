package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// TestRootDocValidates is the strict-from-day-1 contract: every node in
// the tree returned by RootDoc must satisfy every validation rule. As
// commands migrate from cobra-direct construction to *CommandDoc literals
// in subsequent phases, this test grows teeth automatically — the rules
// are absolute and unconditional, no allowlists.
//
// During Phase 1 the root has no Subcommands; identity rules still apply
// to it but the per-kind classifier is exempted (see Validate).
func TestRootDocValidates(t *testing.T) {
	for _, err := range Validate(RootDoc(testAppForDocs())) {
		t.Error(err)
	}
}

// TestRootDocBuilds is the smoke test: the doc tree must produce a
// non-nil cobra.Command tree without panicking. Build calls Validate and
// panics on any error, so this is the strongest "the tree is well-formed"
// signal we can give CI.
func TestRootDocBuilds(t *testing.T) {
	cmd := RootDoc(testAppForDocs()).Build()
	if cmd == nil {
		t.Fatal("Build returned nil")
	}
	if cmd.Name() != "extend" {
		t.Errorf("root Name = %q, want %q", cmd.Name(), "extend")
	}
}

// TestBuildIsStable asserts repeated calls to Build produce equivalent
// trees. Catches accidental dependence on map iteration order or random
// state in rendering.
func TestBuildIsStable(t *testing.T) {
	a := RootDoc(testAppForDocs()).Build()
	b := RootDoc(testAppForDocs()).Build()
	if a.Use != b.Use {
		t.Errorf("Use: %q vs %q", a.Use, b.Use)
	}
	if a.Short != b.Short {
		t.Errorf("Short: %q vs %q", a.Short, b.Short)
	}
	if a.Long != b.Long {
		t.Errorf("Long differs across builds")
	}
	if a.Example != b.Example {
		t.Errorf("Example differs across builds")
	}
}

// TestNameExtractsVerb checks that Name returns the first whitespace-
// delimited token of Use (so "extract <input>" → "extract"), which is
// what cobra and Walk rely on for path construction.
func TestNameExtractsVerb(t *testing.T) {
	cases := map[string]string{
		"extract":         "extract",
		"extract <input>": "extract",
		"runs watch <id>": "runs",
		"webhooks verify": "webhooks",
		"output":          "output",
		"a\tb":            "a",
	}
	for use, want := range cases {
		got := (&CommandDoc{Use: use}).Name()
		if got != want {
			t.Errorf("Name(Use=%q) = %q, want %q", use, got, want)
		}
	}
}

// TestClassifiers asserts the four classifier methods correctly partition
// the space of valid CommandDocs. Each fixture is engineered to be exactly
// one kind (or, for the empty doc, no kind).
func TestClassifiers(t *testing.T) {
	cmd := &CommandDoc{
		Use:     "fixturecmd",
		Summary: "A fixture command for testing",
		RunE:    func(*cobra.Command, []string) error { return nil },
	}
	topic := &CommandDoc{
		Use:        "fixturetopic",
		Summary:    "A fixture topic for testing classifiers",
		RenderBody: func(*CommandDoc) string { return "body" },
	}
	group := &CommandDoc{
		Use:         "fixturegroup",
		Summary:     "A fixture group for testing classifiers",
		Subcommands: []*CommandDoc{cmd},
	}

	checks := []struct {
		name                  string
		d                     *CommandDoc
		isCmd, isTopic, isGrp bool
		isLeaf                bool
	}{
		{"command", cmd, true, false, false, true},
		{"topic", topic, false, true, false, true},
		{"group", group, false, false, true, false},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if got := c.d.IsCommand(); got != c.isCmd {
				t.Errorf("IsCommand = %v, want %v", got, c.isCmd)
			}
			if got := c.d.IsTopic(); got != c.isTopic {
				t.Errorf("IsTopic = %v, want %v", got, c.isTopic)
			}
			if got := c.d.IsGroup(); got != c.isGrp {
				t.Errorf("IsGroup = %v, want %v", got, c.isGrp)
			}
			if got := c.d.IsLeaf(); got != c.isLeaf {
				t.Errorf("IsLeaf = %v, want %v", got, c.isLeaf)
			}
		})
	}
}

// TestWalkOrder asserts Walk visits nodes DFS in declaration order, with
// dotted paths anchored at the root's name.
func TestWalkOrder(t *testing.T) {
	root := &CommandDoc{
		Use:     "root",
		Summary: "Root for testing Walk traversal order",
		Subcommands: []*CommandDoc{
			{Use: "alpha", Summary: "Alpha leaf for Walk testing", RunE: noopRun, Output: jsonJSON, Triggers: triplet("alpha"), Examples: oneExample("extend root alpha")},
			{
				Use:     "beta",
				Summary: "Beta group for Walk testing",
				Details: "x",
				Subcommands: []*CommandDoc{
					{Use: "one", Summary: "Beta one leaf for Walk testing", RunE: noopRun, Output: jsonJSON, Triggers: triplet("beta-one"), Examples: oneExample("extend root beta one")},
					{Use: "two", Summary: "Beta two leaf for Walk testing", RunE: noopRun, Output: jsonJSON, Triggers: triplet("beta-two"), Examples: oneExample("extend root beta two")},
				},
			},
			{Use: "gamma", Summary: "Gamma leaf for Walk testing", RunE: noopRun, Output: jsonJSON, Triggers: triplet("gamma"), Examples: oneExample("extend root gamma")},
		},
	}
	want := []string{"root", "root.alpha", "root.beta", "root.beta.one", "root.beta.two", "root.gamma"}
	got := make([]string, 0, len(want))
	for _, e := range Walk(root) {
		got = append(got, e.Path)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Walk order:\n got: %v\nwant: %v", got, want)
	}
}

// TestBuildProjectsCommandLeaf verifies the cobra projection of a
// fully-specified command leaf. Failures here indicate renderLong,
// renderExamples, or applyAnnotations regressed.
func TestBuildProjectsCommandLeaf(t *testing.T) {
	d := &CommandDoc{
		Use:     "fixturecmd <input>",
		Summary: "A fixture command exercising the projection",
		Triggers: []string{
			"do the fixture thing reliably",
			"test the projection function output",
			"verify build output is structurally correct",
		},
		WhenToUse: "Use when validating the projection.",
		Details:   "Detailed reference body for the fixture command.",
		Examples: []Example{
			{Label: "Basic", Cmd: "extend fixturecmd foo"},
			{Label: "Async", Cmd: "extend fixturecmd foo --wait=false", Note: "Returns the run ID immediately."},
		},
		Gotchas:  []string{"Don't forget to bring a towel."},
		SeeAlso:  []string{},
		Output:   OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileShort, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
		RunE:     noopRun,
	}

	// Build a self-rooted cobra tree to exercise the recursion path.
	root := &CommandDoc{
		Use:         "extend",
		Summary:     "CLI fixture root for projection testing",
		Details:     "x",
		Subcommands: []*CommandDoc{d},
	}
	cmd := root.Build()
	leaf, _, err := cmd.Find([]string{"fixturecmd"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if leaf.Use != "fixturecmd <input>" {
		t.Errorf("Use = %q", leaf.Use)
	}
	if leaf.Short != "A fixture command exercising the projection" {
		t.Errorf("Short = %q", leaf.Short)
	}
	for _, want := range []string{
		"Use when validating the projection.",
		"Detailed reference body",
		"Notes:",
		"Don't forget to bring a towel.",
	} {
		if !strings.Contains(leaf.Long, want) {
			t.Errorf("Long missing %q:\n%s", want, leaf.Long)
		}
	}
	for _, want := range []string{"# Basic", "extend fixturecmd foo", "# Async", "Returns the run ID immediately."} {
		if !strings.Contains(leaf.Example, want) {
			t.Errorf("Example missing %q:\n%s", want, leaf.Example)
		}
	}

	wantAnns := map[string]string{
		AnnotOutputTTY:             "json",
		AnnotOutputPipe:            "json",
		AnnotWaitProfile:           "short",
		AnnotWaitDefault:           "true",
		AnnotLifecycleFailureCodes: "FAILED,CANCELLED",
		AnnotTriggers:              "do the fixture thing reliably,test the projection function output,verify build output is structurally correct",
	}
	for k, want := range wantAnns {
		if got := leaf.Annotations[k]; got != want {
			t.Errorf("Annotations[%q] = %q, want %q", k, got, want)
		}
	}
}

// TestBuildProjectsTopic asserts that topics produce a cobra command
// whose Long is the rendered body and whose RunE/HelpFunc both print
// that body. Confirms `extend <topic>` and `extend help <topic>` agree.
func TestBuildProjectsTopic(t *testing.T) {
	body := "TOPIC BODY GOES HERE"
	topic := &CommandDoc{
		Use:     "fixtuetopic",
		Summary: "A fixture topic exercising the projection",
		Group:   "Help topics",
		Triggers: []string{
			"check the topic body renderer",
			"verify topic projection wiring",
			"confirm extend help topic resolves",
		},
		RenderBody: func(*CommandDoc) string { return body },
	}
	root := &CommandDoc{
		Use:         "extend",
		Summary:     "CLI fixture root for topic projection",
		Details:     "x",
		Subcommands: []*CommandDoc{topic},
	}
	cmd := root.Build()
	leaf, _, err := cmd.Find([]string{"fixtuetopic"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if leaf.Long != body {
		t.Errorf("Long = %q, want %q", leaf.Long, body)
	}
	if leaf.GroupID != HelpTopicGroupID {
		t.Errorf("GroupID = %q, want %q", leaf.GroupID, HelpTopicGroupID)
	}
	if leaf.Annotations[HelpTopicAnnotation] != "true" {
		t.Errorf("HelpTopicAnnotation not set: %v", leaf.Annotations)
	}
}

// TestBuildPanicsOnInvalidDoc verifies the validation contract: malformed
// CommandDocs fail loudly at Build time so contract violations can never
// reach a CLI user.
func TestBuildPanicsOnInvalidDoc(t *testing.T) {
	bad := &CommandDoc{
		Use:     "broken",
		Summary: "Too short", // < 10 chars triggers Summary length error
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Build did not panic on invalid doc")
		}
		if !strings.Contains(r.(string), "Summary length") {
			t.Errorf("panic message %q does not mention Summary length", r)
		}
	}()
	bad.Build()
}

// TestValidateRejectsAmbiguousKind checks the classifier guard: a doc
// that sets multiple of (RunE, Subcommands, RenderBody) is rejected even
// before Build. RenderBody dominates the classifier (a doc with it set is
// a topic), so the ambiguity surfaces via the topic-specific rules
// "must not set RunE" and "must not have Subcommands". Either is
// sufficient to catch the conflict.
func TestValidateRejectsAmbiguousKind(t *testing.T) {
	ambiguous := &CommandDoc{
		Use:     "ambig",
		Summary: "Ambiguous fixture for the classifier test",
		RunE:    noopRun, // command...
		Subcommands: []*CommandDoc{ // ...with subcommands AND RenderBody
			{Use: "child", Summary: "Child of an ambiguous fixture", Details: "x", RunE: noopRun, Output: jsonJSON, Triggers: triplet("child"), Examples: oneExample("extend ambig child")},
		},
		RenderBody: func(*CommandDoc) string { return "" },
	}
	root := &CommandDoc{
		Use:         "extend",
		Summary:     "Root for ambiguous-kind validation test",
		Details:     "x",
		Subcommands: []*CommandDoc{ambiguous},
	}
	errs := Validate(root)
	wantSubstrings := []string{"must not set RunE", "must not have Subcommands"}
	for _, want := range wantSubstrings {
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected an error containing %q; got: %v", want, errs)
		}
	}
}

// TestValidateRejectsDuplicateTriggers asserts the cross-doc uniqueness
// rule catches identical trigger phrases on two different leaves.
func TestValidateRejectsDuplicateTriggers(t *testing.T) {
	shared := []string{
		"identical phrase across two commands",
		"another shared phrase that should fail",
		"third shared phrase to satisfy minimum",
	}
	root := &CommandDoc{
		Use:     "extend",
		Summary: "Root for duplicate-trigger validation test",
		Details: "x",
		Subcommands: []*CommandDoc{
			{Use: "alpha", Summary: "Alpha leaf with shared triggers", RunE: noopRun, Output: jsonJSON, Triggers: shared, Examples: oneExample("extend alpha"), Details: "x"},
			{Use: "beta", Summary: "Beta leaf with same shared triggers", RunE: noopRun, Output: jsonJSON, Triggers: shared, Examples: oneExample("extend beta"), Details: "x"},
		},
	}
	found := false
	for _, e := range Validate(root) {
		if strings.Contains(e.Error(), "claimed by both") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'claimed by both' duplicate-trigger error")
	}
}

// TestValidateRejectsUnresolvedSeeAlso asserts SeeAlso entries pointing
// at non-existent commands are caught.
func TestValidateRejectsUnresolvedSeeAlso(t *testing.T) {
	root := &CommandDoc{
		Use:     "extend",
		Summary: "Root for unresolved-SeeAlso validation test",
		Details: "x",
		Subcommands: []*CommandDoc{
			{
				Use:      "alpha",
				Summary:  "Alpha leaf citing a non-existent peer",
				RunE:     noopRun,
				Output:   jsonJSON,
				Triggers: triplet("alpha-see-also"),
				Examples: oneExample("extend alpha"),
				SeeAlso:  []string{"does-not-exist"},
				Details:  "x",
			},
		},
	}
	found := false
	for _, e := range Validate(root) {
		if strings.Contains(e.Error(), "does not resolve") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a SeeAlso resolution error")
	}
}

// testAppForDocs returns an *App suitable for constructing the doc tree
// in validation tests. It has a nil NewClient: Phase 1 has no commands
// in the tree to run, so no client is ever needed.
func testAppForDocs() *App { return &App{} }

// TestGroupsRejectUnknownSubcommands pins the exit contract for argv
// shapes that don't resolve to a runnable leaf: removed top-level
// groups (runs/run/batches), typed subcommands a kind doesn't have
// (parse runs cancel, edit runs list, workflows batches), and any
// other unknown subcommand of a group. Each must surface an error
// (non-zero exit) rather than printing help and exiting 0 — scripts
// gate on exit status, and a silent success here means a watch or
// cancel that never happened.
func TestGroupsRejectUnknownSubcommands(t *testing.T) {
	cases := []struct {
		args []string
		want string // substring of the error message
	}{
		{[]string{"runs", "watch", "exr_x"}, "extend <extract|parse|classify|split|edit|workflows> runs"},
		{[]string{"run", "in.pdf"}, "extend workflows run"},
		{[]string{"batches", "get", "bpr_x"}, "extend <extract|parse|classify|split> batches"},
		{[]string{"parse", "runs", "cancel", "pr_x"}, `unknown command "cancel" for "extend parse runs"`},
		{[]string{"edit", "runs", "list"}, `unknown command "list" for "extend edit runs"`},
		{[]string{"extract", "runs", "update", "exr_x"}, `unknown command "update" for "extend extract runs"`},
		{[]string{"workflows", "batches", "get", "b_x"}, `unknown command "batches" for "extend workflows"`},
		{[]string{"bogus"}, `unknown command "bogus" for "extend"`},
	}
	for _, tc := range cases {
		ta := newTestApp(t, nil)
		root := RootDoc(ta.app).Build()
		root.SilenceUsage = true
		root.SilenceErrors = true
		root.SetOut(ta.out)
		root.SetErr(ta.errOut)
		root.SetArgs(tc.args)
		err := root.Execute()
		if err == nil {
			t.Errorf("extend %s: err = nil; want unknown-command error", strings.Join(tc.args, " "))
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("extend %s: error %q does not contain %q", strings.Join(tc.args, " "), err, tc.want)
		}
	}

	// A bare group still shows help and exits 0.
	ta := newTestApp(t, nil)
	root := RootDoc(ta.app).Build()
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetOut(ta.out)
	root.SetErr(ta.errOut)
	root.SetArgs([]string{"extract", "runs"})
	if err := root.Execute(); err != nil {
		t.Errorf("bare group 'extend extract runs': err = %v; want nil (help)", err)
	}
	if !strings.Contains(ta.out.String(), "Available Commands") {
		t.Error("bare group 'extend extract runs' did not print help")
	}
}

func noopRun(*cobra.Command, []string) error { return nil }

var jsonJSON = OutputSpec{TTY: OutputJSON, Pipe: OutputJSON}

// triplet returns three distinct, well-formed triggers built from a
// stem; sufficient to satisfy the ≥3 / ≥10 chars / lowercase rules.
func triplet(stem string) []string {
	return []string{
		stem + " trigger phrase one",
		stem + " trigger phrase two",
		stem + " trigger phrase three",
	}
}

// oneExample returns a single-element Example slice with the given
// command line, suitable for fixture leaves.
func oneExample(cmd string) []Example {
	return []Example{{Label: "Basic", Cmd: cmd}}
}
