package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

	for _, want := range []string{"name: extend-cli", "description: "} {
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

		// Processor-family commands (extractors/classifiers/splitters/
		// workflows) are intentionally elided from the per-leaf catalog;
		// they're rendered as a parametric block in the Processor
		// resources section. TestSkillResourceFamiliesShareShape asserts
		// the parametric prose stays accurate.
		if isProcessorFamilyName(segs[0]) {
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

// isProcessorFamilyName reports whether name is the plural noun of one
// of the processor families that the skill renders parametrically.
func isProcessorFamilyName(name string) bool {
	for _, f := range resourceFamilies {
		if f.Plural == name {
			return true
		}
	}
	return false
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

func TestSkillIncludesWorkflowLifecycleRecipe(t *testing.T) {
	body := RenderSkill(RootDoc(testAppForDocs()))
	for _, want := range []string{
		"### Create, deploy, and run a workflow",
		"extend workflows create",
		"extend workflows versions create",
		"extend run invoice.pdf --using",
		"classificationId",
		"cannot use version \"latest\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("skill body missing workflow lifecycle token %q", want)
		}
	}
}

// TestSkillUnderRecommendedTokenBudget enforces the agentskills.io
// specification's recommended ceiling of ~5,000 tokens for SKILL.md
// bodies (the spec's progressive-disclosure principle: just the core
// instructions the agent needs on every run; depth lives behind
// references/ or — for this skill — `extend help <topic>` and
// `extend <command> --help`).
//
// Tokenizers vary slightly per LLM family; English prose mixed with code
// runs about 4 chars/token on Anthropic's and OpenAI's tokenizers, so
// 5,000 tokens corresponds to roughly 20,000 bytes. The constant below
// is the byte budget; if a future tokenizer audit shows the estimate is
// off, adjust the byte budget rather than dropping the test.
//
// When this test fails, the choices are:
//  1. Trim or restructure a section (most common — see the catalog and
//     workflow recipes for highest-leverage cuts).
//  2. Move detailed reference content behind `extend help <topic>` or
//     `extend <command> --help`, then update the dig-deeper section to
//     point the agent there.
//  3. If the new content is genuinely high-leverage and there is no
//     better home, raise the budget *deliberately* with a comment
//     explaining why.
func TestSkillUnderRecommendedTokenBudget(t *testing.T) {
	const recommendedTokens = 5000
	const charsPerToken = 4
	const recommendedBytes = recommendedTokens * charsPerToken

	body := RenderSkill(RootDoc(testAppForDocs()))
	if got := len(body); got > recommendedBytes {
		t.Errorf("skill body is %d bytes (~%d tokens at %d chars/token); spec recommends <=%d tokens. Trim a section, move detail behind `extend <cmd> --help`, or raise the budget deliberately.",
			got, got/charsPerToken, charsPerToken, recommendedTokens)
	}
}

// TestRenderSkillLineBudget asserts a soft line budget independent of
// the token budget — some agent catalog UIs render skill bodies inline
// and browseability degrades past several hundred lines.
func TestRenderSkillLineBudget(t *testing.T) {
	body := RenderSkill(RootDoc(testAppForDocs()))
	const softLimitLines = 400
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
	if !strings.HasPrefix(string(data), "---\nname: extend-cli\n") {
		t.Errorf("written file does not start with frontmatter:\n%s", string(data[:64]))
	}
	if !strings.Contains(ta.errOut.String(), "Wrote") {
		t.Errorf("expected Wrote-bytes status on stderr; got: %q", ta.errOut.String())
	}
	if !strings.Contains(ta.errOut.String(), target) {
		t.Errorf("stderr should mention the target path; got: %q", ta.errOut.String())
	}
}

// TestSkillInstallSymlinksIntoClaude verifies the default install also
// links the skill dir into ~/.claude/skills/extend-cli (Claude Code doesn't
// read ~/.agents/skills), and that SKILL.md resolves through the link.
func TestSkillInstallSymlinksIntoClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink install assertions are unix-specific")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "skill", "install")
	cmd.SetArgs(nil) // default target
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "extend-cli", "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md not written to default location: %v", err)
	}
	link := filepath.Join(home, ".claude", "skills", "extend-cli")
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("claude symlink not created: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", link)
	}
	dest, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".agents", "skills", "extend-cli"); dest != want {
		t.Errorf("symlink target = %q, want %q", dest, want)
	}
	if _, err := os.Stat(filepath.Join(link, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not reachable via claude link: %v", err)
	}
	if !strings.Contains(ta.errOut.String(), "Linked") {
		t.Errorf("expected Linked status on stderr; got: %q", ta.errOut.String())
	}

	// Idempotent: a second default install replaces the symlink cleanly.
	ta2 := newTestApp(t, newFakeServer(t, nil))
	cmd2 := findCmd(t, ta2.app, "skill", "install")
	cmd2.SetArgs(nil)
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("second install (idempotency): %v", err)
	}
}

// TestSkillInstallClaudeLinkSkipsRealDir ensures a real directory already
// at ~/.claude/skills/extend-cli is left untouched (not clobbered), the link
// step is skipped with a warning, and the install still succeeds.
func TestSkillInstallClaudeLinkSkipsRealDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink install assertions are unix-specific")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	realDir := filepath.Join(home, ".claude", "skills", "extend-cli")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(realDir, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "skill", "install")
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install should still succeed despite link skip: %v", err)
	}

	fi, err := os.Lstat(realDir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("real directory was replaced by a symlink (clobbered)")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel file removed under the real dir: %v", err)
	}
	if !strings.Contains(ta.errOut.String(), "Skipped") {
		t.Errorf("expected Skipped warning on stderr; got: %q", ta.errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "extend-cli", "SKILL.md")); err != nil {
		t.Errorf("SKILL.md should still be written: %v", err)
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
	if !strings.HasPrefix(ta.out.String(), "---\nname: extend-cli\n") {
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
	if !strings.HasPrefix(ta.out.String(), "---\nname: extend-cli\n") {
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
	want := filepath.Join(home, ".agents", "skills", "extend-cli", "SKILL.md")
	if got != want {
		t.Errorf("defaultSkillTarget() = %q, want %q", got, want)
	}
}

// Guard tests for hand-written claims in the rendered skill body.
//
// The renderer mixes auto-generated content (catalog, topic list, action
// table, env vars) with hand-curated prose (description verbs, resource
// family list, wait/async claims, pagination example, workflow recipes).
// The auto-generated content stays correct as long as the typed tree is
// correct. The hand-curated content can drift; these tests guard each
// claim against the typed tree it references.

// TestSkillDescriptionUnderSpecLimit asserts the rendered description
// stays under the agentskills.io 1024-char hard limit. Catches drift if
// new descriptionVerbs entries or wordier disambiguation examples push
// the description over the spec limit.
func TestSkillDescriptionUnderSpecLimit(t *testing.T) {
	const specLimit = 1024
	if got := len(renderDescription()); got > specLimit {
		t.Errorf("description is %d chars, exceeds agentskills.io spec limit of %d", got, specLimit)
	}
}

// TestSkillDescriptionVerbsResolve asserts every entry in the
// descriptionVerbs table maps to a real top-level command in the doc
// tree. If a verb is renamed or removed without updating this table, the
// test fails — preventing the description from claiming capabilities the
// CLI no longer has.
func TestSkillDescriptionVerbsResolve(t *testing.T) {
	root := RootDoc(testAppForDocs())
	have := map[string]bool{}
	for _, sub := range root.Subcommands {
		have[sub.Name()] = true
	}
	for _, v := range descriptionVerbs {
		if !have[v.Command] {
			t.Errorf("descriptionVerbs entry %q references command %q, not found in root.Subcommands", v.Phrase, v.Command)
		}
	}
}

// TestSkillResourceFamiliesShareShape asserts every family in the
// resourceFamilies table exists at the top level and exposes exactly the
// processorFamilyCommands set of leaves. The skill renders these
// parametrically, claiming "share an identical seven-command shape"; if a
// family adds, removes, or renames a command, the parametric prose lies
// — and this test fails until either the prose is updated or the family
// is brought back into shape.
func TestSkillResourceFamiliesShareShape(t *testing.T) {
	root := RootDoc(testAppForDocs())
	have := map[string]*CommandDoc{}
	for _, sub := range root.Subcommands {
		have[sub.Name()] = sub
	}

	for _, f := range resourceFamilies {
		fam, ok := have[f.Plural]
		if !ok {
			t.Errorf("family %q not in root.Subcommands", f.Plural)
			continue
		}
		got := flattenCommandPaths(fam, "")
		want := append([]string(nil), processorFamilyCommands...)

		gotSorted := append([]string(nil), got...)
		sortStrings(gotSorted)
		wantSorted := append([]string(nil), want...)
		sortStrings(wantSorted)
		if !sliceEqual(gotSorted, wantSorted) {
			t.Errorf("family %q has commands %v, expected %v", f.Plural, gotSorted, wantSorted)
		}
	}
}

// flattenCommandPaths returns the relative space-separated paths of
// every IsCommand leaf under d. Used to compare a resource family's
// shape to processorFamilyCommands.
func flattenCommandPaths(d *CommandDoc, prefix string) []string {
	var out []string
	for _, sub := range d.Subcommands {
		path := sub.Name()
		if prefix != "" {
			path = prefix + " " + sub.Name()
		}
		if sub.IsCommand() {
			out = append(out, path)
		}
		out = append(out, flattenCommandPaths(sub, path)...)
	}
	return out
}

// sortStrings sorts in-place; stdlib sort.Strings via tiny wrapper so the
// test file's import surface stays small.
func sortStrings(s []string) {
	// Insertion sort: simple, stable, fine for tiny slices.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSkillWaitClaimsMatchTree asserts the Wait/async section's claims
// about which verbs default to wait match each command's
// CommandDoc.Wait.DefaultsToWait. If a command's default behaviour
// changes, the prose section becomes wrong — this test catches the
// mismatch.
func TestSkillWaitClaimsMatchTree(t *testing.T) {
	root := RootDoc(testAppForDocs())
	subs := map[string]*CommandDoc{}
	for _, sub := range root.Subcommands {
		subs[sub.Name()] = sub
	}

	for _, verb := range waitDefaultVerbs {
		d, ok := subs[verb]
		if !ok {
			t.Errorf("waitDefaultVerbs entry %q not in root.Subcommands", verb)
			continue
		}
		if d.Wait == nil {
			t.Errorf("verb %q has no Wait spec; skill claims it waits by default", verb)
			continue
		}
		if !d.Wait.DefaultsToWait {
			t.Errorf("verb %q: Wait.DefaultsToWait = false, skill claims it waits by default", verb)
		}
	}

	for _, verb := range asyncDefaultVerbs {
		d, ok := subs[verb]
		if !ok {
			t.Errorf("asyncDefaultVerbs entry %q not in root.Subcommands", verb)
			continue
		}
		if d.Wait == nil {
			// nil Wait means no polling at all; that's not "async by
			// default" the way the skill means it (which is "starts the
			// run and returns immediately"). Caller should adjust either
			// the skill prose or the command's WaitSpec.
			t.Errorf("verb %q has no Wait spec; skill claims it is async by default", verb)
			continue
		}
		if d.Wait.DefaultsToWait {
			t.Errorf("verb %q: Wait.DefaultsToWait = true, skill claims it is async by default", verb)
		}
	}
}

// TestSkillPaginationExampleIsValid asserts the bash example in the
// Pagination section uses a real command with real flags. Catches the
// case where the example's command is renamed or a referenced flag is
// removed from the underlying command.
//
// Uses NewRoot rather than RootDoc(...).Build() because some referenced
// flags (notably --output / -o, --jq) are root-level *persistent* flags
// added by NewRoot, not by Build. The skill body documents these as if
// they apply uniformly to every command — and they do, but only via the
// production wiring path.
func TestSkillPaginationExampleIsValid(t *testing.T) {
	root := NewRoot()

	cmd, _, err := root.Find(paginationExampleCommand)
	if err != nil {
		t.Fatalf("paginationExampleCommand %v: %v", paginationExampleCommand, err)
	}

	for _, name := range paginationExampleFlags {
		if flagFindAnywhere(cmd, name) == nil {
			t.Errorf("flag --%s not found on `extend %s` (incl. persistent flags up the tree)", name, strings.Join(paginationExampleCommand, " "))
		}
	}
}

// flagFindAnywhere walks up from cmd through every parent looking for a
// flag with the given long name on local flags or any ancestor's
// persistent flags. Returns nil if none found.
func flagFindAnywhere(cmd *cobra.Command, name string) *pflag.Flag {
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.Flags().Lookup(name); f != nil {
			return f
		}
		if f := c.PersistentFlags().Lookup(name); f != nil {
			return f
		}
	}
	return nil
}

// TestSkillWorkflowsReferenceRealCommands extracts every `extend
// <command...>` token from the Common Workflows section of the rendered
// body and asserts each resolves to a real command in the doc tree.
// Catches command renames or removals that would invalidate a recipe.
func TestSkillWorkflowsReferenceRealCommands(t *testing.T) {
	body := RenderSkill(RootDoc(testAppForDocs()))

	const startMarker = "## Common workflows\n"
	const endMarker = "## Command reference\n"
	startIdx := strings.Index(body, startMarker)
	endIdx := strings.Index(body, endMarker)
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		t.Fatalf("could not locate Common workflows section in body (start=%d end=%d)", startIdx, endIdx)
	}
	section := body[startIdx:endIdx]

	root := RootDoc(testAppForDocs()).Build()

	// Walk the section looking for `extend <token>...` invocations.
	// We accept tokens that are command-path segments (lowercase
	// alphanumeric or hyphenated); flags (start with -) and shell vars
	// (start with $) terminate the path.
	for _, line := range strings.Split(section, "\n") {
		idx := 0
		for {
			i := strings.Index(line[idx:], "extend ")
			if i == -1 {
				break
			}
			start := idx + i + len("extend ")
			// Walk forward collecting path segments.
			var segs []string
			pos := start
			for pos < len(line) {
				// Skip leading whitespace if any.
				for pos < len(line) && line[pos] == ' ' {
					pos++
				}
				if pos >= len(line) {
					break
				}
				// Stop at flag, redirect, pipe, etc.
				c := line[pos]
				if c == '-' || c == '$' || c == '|' || c == '>' || c == '\\' || c == '"' || c == '\'' || c == '`' {
					break
				}
				// Collect alpha/hyphen token.
				tokStart := pos
				for pos < len(line) {
					c := line[pos]
					if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
						break
					}
					pos++
				}
				if pos == tokStart {
					break
				}
				tok := line[tokStart:pos]
				// Heuristic: stop if this looks like an argument value
				// (e.g. file path, ID with prefix). Command path segments
				// are lowercase words without underscores or numbers (except
				// 'workflow_run_' but those are IDs not paths).
				if strings.ContainsAny(tok, "_") {
					break
				}
				if strings.ContainsAny(tok, "0123456789") {
					break
				}
				if !isLikelyCommandSegment(tok) {
					break
				}
				segs = append(segs, tok)
			}
			if len(segs) > 0 {
				if _, _, err := root.Find(segs); err != nil {
					t.Errorf("workflow recipe references `extend %s` which does not resolve in the tree (line: %q)", strings.Join(segs, " "), strings.TrimSpace(line))
				}
			}
			idx = pos
			if idx <= start {
				idx = start + 1 // ensure progress
			}
		}
	}
}

// isLikelyCommandSegment reports whether s looks like a command name
// segment (vs a positional value). Command segments are short lowercase
// words; values often have hyphens with multi-word patterns.
func isLikelyCommandSegment(s string) bool {
	if s == "" {
		return false
	}
	if s[0] >= 'A' && s[0] <= 'Z' {
		// Variable-name-like token (BATCH, X_EXTEND_...). Not a command segment.
		return false
	}
	return true
}
