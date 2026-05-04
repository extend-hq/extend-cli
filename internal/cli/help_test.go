package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestHelpTopicsExist confirms the four canonical topics are registered
// in the production cobra tree built by NewRoot. The test reads the topic
// names from the cobra tree (via helpTopicNames) rather than from RootDoc
// directly so that the production wiring path stays exercised.
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

// TestHelpTopicsRender invokes each topic and checks it produces non-empty
// output without erroring. Catches drift in the RenderBody implementations
// (now in topics.go) and confirms `extend <topic>` works as a runnable
// command after the doc-tree migration.
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
