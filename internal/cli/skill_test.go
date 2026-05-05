package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderSkillFrontmatter checks the YAML frontmatter is well-formed
// and contains the required fields. Agent harnesses parse this region as
// strict YAML; broken frontmatter means the skill is silently skipped.
func TestRenderSkillFrontmatter(t *testing.T) {
	body := RenderSkill(RootDoc(testAppForDocs()))

	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("body does not start with --- frontmatter delimiter; got %q", body[:32])
	}
	end := strings.Index(body[4:], "\n---\n")
	if end == -1 {
		t.Fatal("body has no closing --- frontmatter delimiter")
	}
	front := body[4 : 4+end]

	for _, want := range []string{"name: extend", "description: "} {
		if !strings.Contains(front, want) {
			t.Errorf("frontmatter missing %q:\n%s", want, front)
		}
	}
}

// TestRenderSkillContainsEveryCommandLeaf is the "no command falls out
// of the catalog" check. Every command leaf in the doc tree must appear
// somewhere in the rendered body, identified by its full invocation
// path. If a new command is added but the renderer fails to surface it
// (group misclassification, walk skipping, etc.), this test fails.
func TestRenderSkillContainsEveryCommandLeaf(t *testing.T) {
	root := RootDoc(testAppForDocs())
	body := RenderSkill(root)

	for _, e := range Walk(root) {
		if !e.Doc.IsCommand() {
			continue
		}
		// Skip the skill commands themselves; they live in "Agent surface"
		// which the catalog intentionally elides to avoid recursion.
		if e.Doc.Group == "Agent surface" {
			continue
		}
		// Find the top ancestor's group; only require visibility for
		// non-Help-topics action/inspection/resource leaves.
		segs := strings.SplitN(strings.TrimPrefix(e.Path, root.Name()+"."), ".", 2)
		if len(segs) == 0 {
			continue
		}
		var topGroup string
		for _, sub := range root.Subcommands {
			if sub.Name() == segs[0] {
				topGroup = sub.Group
				break
			}
		}
		if topGroup == "Help topics" || topGroup == "Agent surface" || topGroup == "" {
			continue
		}

		// Build the invocation string we expect in the catalog.
		pathVerbs := strings.ReplaceAll(strings.TrimPrefix(e.Path, root.Name()+"."), ".", " ")
		leafName := e.Doc.Name()
		parentVerbs := strings.TrimSuffix(strings.TrimSuffix(pathVerbs, leafName), " ")
		invocation := e.Doc.Use
		if parentVerbs != "" {
			invocation = parentVerbs + " " + e.Doc.Use
		}
		needle := "`extend " + invocation + "`"
		if !strings.Contains(body, needle) {
			t.Errorf("skill body missing leaf invocation %q (path=%s)", needle, e.Path)
		}
	}
}

// TestRenderSkillTopicsResolve asserts every `extend help <topic>`
// reference in the rendered body points to a real topic in the doc tree.
// Catches drift if a topic is renamed or removed without updating the
// renderer's hand-written sections.
func TestRenderSkillTopicsResolve(t *testing.T) {
	root := RootDoc(testAppForDocs())
	body := RenderSkill(root)

	known := map[string]bool{}
	for _, e := range Walk(root) {
		if e.Doc.IsTopic() {
			known[e.Doc.Name()] = true
		}
	}

	// Find every "extend help <token>" reference in the body and check
	// the token resolves.
	idx := 0
	for {
		i := strings.Index(body[idx:], "extend help ")
		if i == -1 {
			break
		}
		start := idx + i + len("extend help ")
		// Topic name is alphanumeric, terminated by ` or whitespace.
		end := start
		for end < len(body) {
			c := body[end]
			if c == '`' || c == ' ' || c == '\n' || c == '|' {
				break
			}
			end++
		}
		token := body[start:end]
		if token != "" && token != "<topic>" && !known[token] {
			t.Errorf("body references unknown topic %q (after %q)", token, body[max(0, start-30):end])
		}
		idx = end
	}
}

// TestRenderSkillIsStable: pure function. Two calls produce the same body.
func TestRenderSkillIsStable(t *testing.T) {
	a := RenderSkill(RootDoc(testAppForDocs()))
	b := RenderSkill(RootDoc(testAppForDocs()))
	if a != b {
		t.Errorf("RenderSkill is not stable across calls (length: %d vs %d)", len(a), len(b))
	}
}

// TestRenderSkillSizeBudget asserts the body stays under a soft budget so
// it fits in agent skill catalogs without summarisation. Agentskills.io
// recommends ≤5,000 tokens; ~4 chars/token gives a 20,000-byte target.
// We allow some headroom (30,000) before alarming; the current body is
// ~16 KB so this is a regression guard, not a tight constraint.
func TestRenderSkillSizeBudget(t *testing.T) {
	body := RenderSkill(RootDoc(testAppForDocs()))
	const softLimitBytes = 30000
	if got := len(body); got > softLimitBytes {
		t.Errorf("skill body grew to %d bytes (soft limit %d). Either trim a section or raise the limit deliberately.", got, softLimitBytes)
	}
}

// TestRenderSkillLineBudget asserts a soft line budget. Some agent
// catalog UIs render skill bodies inline; very long bodies hurt
// browseability. Same shape as the byte budget — guard, not tight rule.
func TestRenderSkillLineBudget(t *testing.T) {
	body := RenderSkill(RootDoc(testAppForDocs()))
	const softLimitLines = 600
	if got := strings.Count(body, "\n"); got > softLimitLines {
		t.Errorf("skill body grew to %d lines (soft limit %d).", got, softLimitLines)
	}
}

// TestSkillInstallWritesToTarget exercises the install subcommand's
// happy path: `--target <path>` writes the SKILL.md to that file, creates
// missing parent directories, and prints a success line to stderr.
func TestSkillInstallWritesToTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "deeply", "nested", "SKILL.md")

	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "skill", "install")
	cmd.SetArgs([]string{"--target", target})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\nname: extend\n") {
		t.Errorf("written file does not start with frontmatter:\n%s", string(data[:64]))
	}
	if !strings.Contains(ta.errOut.String(), "Wrote") {
		t.Errorf("expected Wrote-bytes status on stderr; got: %q", ta.errOut.String())
	}
	if !strings.Contains(ta.errOut.String(), target) {
		t.Errorf("stderr should mention the target path; got: %q", ta.errOut.String())
	}
}

// TestSkillInstallStdoutPath: `--target -` streams to stdout instead of
// writing a file. Documented behavior; agents may pipe it.
func TestSkillInstallStdoutPath(t *testing.T) {
	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "skill", "install")
	cmd.SetArgs([]string{"--target", "-"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.HasPrefix(ta.out.String(), "---\nname: extend\n") {
		t.Errorf("stdout should contain the rendered SKILL.md; got %q", ta.out.String()[:min(64, len(ta.out.String()))])
	}
	// And nothing on stderr — install -t - is silent on success.
	if strings.Contains(ta.errOut.String(), "Wrote") {
		t.Errorf("stdout-target install should not print Wrote-status to stderr; got: %q", ta.errOut.String())
	}
}

// TestSkillCommandPrintsToStdout: the bare `extend skill` command prints
// the body to stdout and emits no stderr.
func TestSkillCommandPrintsToStdout(t *testing.T) {
	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "skill")
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill: %v", err)
	}
	if !strings.HasPrefix(ta.out.String(), "---\nname: extend\n") {
		t.Errorf("stdout should contain rendered SKILL.md; got %q", ta.out.String()[:min(64, len(ta.out.String()))])
	}
	if ta.errOut.Len() != 0 {
		t.Errorf("`extend skill` should not write to stderr; got: %q", ta.errOut.String())
	}
}

// TestDefaultSkillTargetIsCrossClient asserts the default install target
// matches the agentskills.io cross-client convention. Locking this in a
// test means renaming or moving the default requires deliberate intent.
func TestDefaultSkillTargetIsCrossClient(t *testing.T) {
	got, err := defaultSkillTarget()
	if err != nil {
		t.Fatalf("defaultSkillTarget: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".agents", "skills", "extend", "SKILL.md")
	if got != want {
		t.Errorf("defaultSkillTarget() = %q, want %q", got, want)
	}
}
