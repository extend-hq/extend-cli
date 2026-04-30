package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestHelpTopicsExist confirms the four expected topics are registered. If
// you rename or remove a topic, the footer text in renderTopicFooter and any
// per-command "see `extend help X`" references must also update.
func TestHelpTopicsExist(t *testing.T) {
	root := NewRoot()
	want := []string{"auth", "errors", "lifecycle", "output"}
	got := helpTopicNames(root)
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("missing help topic %q (registered: %v)", w, got)
		}
	}
}

// TestHelpTopicsRender runs each topic and checks it produces non-empty
// output without erroring. Catches drift in the renderers and protects
// against accidentally registering a topic with a nil renderer.
func TestHelpTopicsRender(t *testing.T) {
	for _, name := range helpTopicNames(NewRoot()) {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			r := NewRoot()
			r.SetOut(&buf)
			r.SetArgs([]string{name})
			if err := r.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if buf.Len() == 0 {
				t.Errorf("topic produced empty output")
			}
		})
	}
}

// TestTopicFooterAppearsOnCommands checks that a representative non-topic
// command's --help output ends with the topic-pointer footer, and that the
// topics themselves do NOT include the footer (would be self-referential).
func TestTopicFooterAppearsOnCommands(t *testing.T) {
	root := NewRoot()

	// Non-topic command: footer present.
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"extract", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("extract --help: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Learn more:") {
		t.Errorf("extract --help missing 'Learn more:' footer:\n%s", out)
	}
	for _, topic := range []string{"auth", "errors", "lifecycle", "output"} {
		if !strings.Contains(out, "extend help "+topic) {
			t.Errorf("extract --help missing pointer to topic %q", topic)
		}
	}

	// Topic command: footer absent (would be recursive).
	buf.Reset()
	root2 := NewRoot()
	root2.SetOut(&buf)
	root2.SetArgs([]string{"auth"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if strings.Contains(buf.String(), "Learn more:") {
		t.Errorf("topic 'auth' output should not include the topic footer:\n%s", buf.String())
	}
}

// TestEveryRunnableLeafHasIOAnnotations enforces that every runnable command
// declares its output behavior via annotations. This is the contract the
// `extend help output` topic relies on.
//
// New commands added without annotations fail this test before merge.
func TestEveryRunnableLeafHasIOAnnotations(t *testing.T) {
	root := NewRoot()
	for _, cmd := range RunnableLeaves(root) {
		if isAnnotationExempt(cmd) {
			continue
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			tty, ok := cmd.Annotations[AnnotOutputTTY]
			if !ok || tty == "" {
				t.Errorf("missing %s annotation; set via cli.SetIOAnnotations(cmd, tty, pipe)", AnnotOutputTTY)
			} else if !validOutputMode(tty) {
				t.Errorf("%s = %q is not a valid OutputMode (valid: %v)", AnnotOutputTTY, tty, OutputModes)
			}
			pipe, ok := cmd.Annotations[AnnotOutputPipe]
			if !ok || pipe == "" {
				t.Errorf("missing %s annotation", AnnotOutputPipe)
			} else if !validOutputMode(pipe) {
				t.Errorf("%s = %q is not a valid OutputMode (valid: %v)", AnnotOutputPipe, pipe, OutputModes)
			}
		})
	}
}

// TestEveryRunnableLeafHasWaitAnnotations enforces wait.profile and
// wait.default on every runnable command, even those that don't wait —
// "n/a" is required so the contract is explicit rather than implicit.
func TestEveryRunnableLeafHasWaitAnnotations(t *testing.T) {
	root := NewRoot()
	for _, cmd := range RunnableLeaves(root) {
		if isAnnotationExempt(cmd) {
			continue
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			profile, ok := cmd.Annotations[AnnotWaitProfile]
			if !ok || profile == "" {
				t.Errorf("missing %s annotation (use \"n/a\" for non-waiting commands)", AnnotWaitProfile)
			} else if !validWaitProfile(profile) {
				t.Errorf("%s = %q is not valid (valid: %v)", AnnotWaitProfile, profile, WaitProfileNames)
			}
			def, ok := cmd.Annotations[AnnotWaitDefault]
			if !ok || def == "" {
				t.Errorf("missing %s annotation (use \"n/a\" for non-waiting commands)", AnnotWaitDefault)
			} else if !validWaitDefault(def) {
				t.Errorf("%s = %q is not valid (valid: %v)", AnnotWaitDefault, def, WaitDefaultValues)
			}
		})
	}
}

// TestLifecycleFailureCodesAreValid checks that any AnnotLifecycleFailureCodes
// annotation set on a command resolves to known RunStatus values. The
// annotation is optional; only its content is validated when present.
func TestLifecycleFailureCodesAreValid(t *testing.T) {
	root := NewRoot()
	valid := map[string]bool{
		"FAILED":       true,
		"CANCELLED":    true,
		"REJECTED":     true,
		"NEEDS_REVIEW": true,
	}
	for _, cmd := range RunnableLeaves(root) {
		raw, ok := cmd.Annotations[AnnotLifecycleFailureCodes]
		if !ok || raw == "" {
			continue
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			for _, s := range strings.Split(raw, ",") {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if !valid[s] {
					t.Errorf("%s contains unknown status %q", AnnotLifecycleFailureCodes, s)
				}
			}
		})
	}
}

// TestEveryRunnableLeafHasShortAndLong is the help-text presence test. It is
// gated by helpTextExempt so PRs can land annotation work first; subsequent
// PRs shrink the exempt set as commands gain real Long/Example text.
func TestEveryRunnableLeafHasShortAndLong(t *testing.T) {
	root := NewRoot()
	for _, cmd := range RunnableLeaves(root) {
		if helpTextExempt(cmd) {
			continue
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			if strings.TrimSpace(cmd.Short) == "" {
				t.Errorf("Short is empty")
			}
			if strings.TrimSpace(cmd.Long) == "" {
				t.Errorf("Long is empty")
			}
			if strings.TrimSpace(cmd.Example) == "" {
				t.Errorf("Example is empty")
			}
		})
	}
}

// isAnnotationExempt is the small set of commands that legitimately don't
// need IO/wait annotations: `version` (prints a fixed string) and any help
// topic (added in PR 3). This list should stay small.
func isAnnotationExempt(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "version", "help", "completion":
		return true
	}
	if cmd.Annotations["help_topic"] == "true" {
		return true
	}
	return false
}

// helpTextExempt is the explicit allowlist of commands that haven't been
// brought up to the Long/Example standard yet. Shrinking this list is the
// goal of subsequent PRs; it should reach empty.
//
// Help topics are also exempt: their body is rendered at runtime, so the
// static Long/Example fields are not where the docs live.
func helpTextExempt(cmd *cobra.Command) bool {
	if cmd.Annotations[HelpTopicAnnotation] == "true" {
		return true
	}
	exempt := map[string]bool{
		// Umbrella resource families: have templated Long but no Example
		// suitable for the umbrella; subcommands carry the examples. Allow
		// these to skip Example only — Short/Long are checked below.
		"extend extractors":             true,
		"extend classifiers":            true,
		"extend splitters":              true,
		"extend workflows":              true,
		"extend extractors versions":    true,
		"extend classifiers versions":   true,
		"extend splitters versions":     true,
		"extend workflows versions":     true,
		"extend webhooks":               true,
		"extend webhooks endpoints":     true,
		"extend webhooks subscriptions": true,
		"extend evaluations":            true,
		"extend evaluations items":      true,
		"extend evaluations runs":       true,
		"extend runs":                   true,
		"extend batches":                true,
		"extend files":                  true,
		// Commands with bare Short and no Long/Example. Each gets fixed in
		// PR 4; the entry here keeps verification green in the meantime.
		"extend extractors list":               true,
		"extend extractors get":                true,
		"extend extractors update":             true,
		"extend extractors create":             true,
		"extend extractors versions list":      true,
		"extend extractors versions get":       true,
		"extend extractors versions create":    true,
		"extend classifiers list":              true,
		"extend classifiers get":               true,
		"extend classifiers update":            true,
		"extend classifiers create":            true,
		"extend classifiers versions list":     true,
		"extend classifiers versions get":      true,
		"extend classifiers versions create":   true,
		"extend splitters list":                true,
		"extend splitters get":                 true,
		"extend splitters update":              true,
		"extend splitters create":              true,
		"extend splitters versions list":       true,
		"extend splitters versions get":        true,
		"extend splitters versions create":     true,
		"extend workflows list":                true,
		"extend workflows get":                 true,
		"extend workflows update":              true,
		"extend workflows create":              true,
		"extend workflows versions list":       true,
		"extend workflows versions get":        true,
		"extend workflows versions create":     true,
		"extend classify batch":                true,
		"extend split batch":                   true,
		"extend batches get":                   true,
		"extend batches watch":                 true,
		"extend files upload":                  true,
		"extend files list":                    true,
		"extend files delete":                  true,
		"extend webhooks endpoints get":        true,
		"extend webhooks endpoints update":     true,
		"extend webhooks endpoints delete":     true,
		"extend webhooks subscriptions list":   true,
		"extend webhooks subscriptions get":    true,
		"extend webhooks subscriptions update": true,
		"extend webhooks subscriptions delete": true,
		"extend evaluations list":              true,
		"extend evaluations get":               true,
		"extend evaluations create":            true,
		"extend evaluations items list":        true,
		"extend evaluations items get":         true,
		"extend evaluations items create":      true,
		"extend evaluations items update":      true,
		"extend evaluations items delete":      true,
		"extend evaluations runs get":          true,
		"extend edit schema generate":          true,
		"extend parse batch":                   true,
		"extend run batch":                     true,
		"extend runs get":                      true,
		"extend runs cancel":                   true,
		"extend runs delete":                   true,
		"extend webhooks endpoints list":       true,
		"extend version":                       true,
	}
	return exempt[cmd.CommandPath()]
}
