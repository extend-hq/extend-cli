package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestRenderSkillFrontmatter checks the YAML frontmatter is well-formed
// and contains the required fields. Agent harnesses parse this region
// as strict YAML; broken frontmatter means the skill is silently
// skipped.
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

// TestRenderSkillBundleContainsEveryCommandLeaf is the "no command
// falls out of the catalog" check. Every command leaf in the doc tree
// must appear somewhere in the bundle — either the slim SKILL.md
// (action verbs) or one of the references/ files. If a new command is
// added but neither surface picks it up, this test fails.
func TestRenderSkillBundleContainsEveryCommandLeaf(t *testing.T) {
	root := RootDoc(testAppForDocs())
	bundle := RenderSkillBundle(root)

	// Concatenate every surface the agent might load. Anywhere is
	// enough — we don't care which surface, only that the command
	// shows up somewhere reachable.
	corpus := bundle.SkillMD
	for _, body := range bundle.References {
		corpus += "\n" + body
	}

	for _, e := range Walk(root) {
		if !e.Doc.IsCommand() {
			continue
		}
		// Skip the skill commands themselves; they live in
		// "Agent surface" which the catalog intentionally elides.
		if e.Doc.Group == "Agent surface" {
			continue
		}
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
		// Processor-family commands render parametrically.
		if isProcessorFamilyName(segs[0]) {
			continue
		}

		pathVerbs := strings.ReplaceAll(strings.TrimPrefix(e.Path, root.Name()+"."), ".", " ")
		leafName := e.Doc.Name()
		parentVerbs := strings.TrimSuffix(strings.TrimSuffix(pathVerbs, leafName), " ")
		invocation := e.Doc.Use
		if parentVerbs != "" {
			invocation = parentVerbs + " " + e.Doc.Use
		}
		needle := "`extend " + invocation + "`"
		if !strings.Contains(corpus, needle) {
			t.Errorf("skill bundle missing leaf invocation %q (path=%s)", needle, e.Path)
		}
	}
}

func isProcessorFamilyName(name string) bool {
	for _, f := range resourceFamilies {
		if f.Plural == name {
			return true
		}
	}
	return false
}

// TestRenderSkillTopicsResolve asserts every `extend help <topic>`
// reference in the rendered body points to a real topic in the doc
// tree.
func TestRenderSkillTopicsResolve(t *testing.T) {
	root := RootDoc(testAppForDocs())
	body := RenderSkill(root)

	known := map[string]bool{}
	for _, e := range Walk(root) {
		if e.Doc.IsTopic() {
			known[e.Doc.Name()] = true
		}
	}

	idx := 0
	for {
		i := strings.Index(body[idx:], "extend help ")
		if i == -1 {
			break
		}
		start := idx + i + len("extend help ")
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

// TestRenderSkillReferencesResolve walks SKILL.md for tokens of the
// form `references/<name>.md` and asserts each one names a file in the
// rendered bundle. Skips bare "references/" matches that aren't
// followed by a real filename (used in prose like "under references/").
func TestRenderSkillReferencesResolve(t *testing.T) {
	root := RootDoc(testAppForDocs())
	bundle := RenderSkillBundle(root)

	tokens := extractReferenceFilenames(bundle.SkillMD)
	if len(tokens) == 0 {
		t.Fatal("SKILL.md has no references/<name>.md tokens; the references section is empty")
	}
	for _, name := range tokens {
		if _, ok := bundle.References[name]; !ok {
			t.Errorf("SKILL.md references %q but bundle has no such file (refs: %v)", "references/"+name, sortedKeys(bundle.References))
		}
	}
}

// extractReferenceFilenames returns every "<name>.md" filename that
// appears immediately after a "references/" prefix in body. Restricts
// the filename charset to [a-z0-9-] + ".md" so prose mentions of bare
// "references/" don't get picked up as bogus tokens.
func extractReferenceFilenames(body string) []string {
	var out []string
	const prefix = "references/"
	idx := 0
	for {
		i := strings.Index(body[idx:], prefix)
		if i == -1 {
			break
		}
		start := idx + i + len(prefix)
		end := start
		for end < len(body) {
			c := body[end]
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
				end++
				continue
			}
			break
		}
		if end > start && strings.HasPrefix(body[end:], ".md") {
			out = append(out, body[start:end]+".md")
			idx = end + len(".md")
			continue
		}
		idx = end
		if idx == start {
			idx = start + 1
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

// TestRenderSkillIsStable: pure function. Two calls produce the same body.
func TestRenderSkillIsStable(t *testing.T) {
	a := RenderSkill(RootDoc(testAppForDocs()))
	b := RenderSkill(RootDoc(testAppForDocs()))
	if a != b {
		t.Errorf("RenderSkill is not stable across calls (length: %d vs %d)", len(a), len(b))
	}
}

// TestSkillUnderRecommendedTokenBudget enforces the agentskills.io
// specification's recommended ceiling of ~5,000 tokens for SKILL.md.
// Detail content moved to references/ doesn't count against this
// budget — the agent loads it on demand, not on every activation.
func TestSkillUnderRecommendedTokenBudget(t *testing.T) {
	const recommendedTokens = 5000
	const charsPerToken = 4
	const recommendedBytes = recommendedTokens * charsPerToken

	body := RenderSkill(RootDoc(testAppForDocs()))
	if got := len(body); got > recommendedBytes {
		t.Errorf("SKILL.md body is %d bytes (~%d tokens at %d chars/token); spec recommends <=%d tokens. Move detail to a reference topic or raise the budget deliberately.",
			got, got/charsPerToken, charsPerToken, recommendedTokens)
	}
}

// TestRenderSkillLineBudget asserts a soft line budget for the slim
// SKILL.md body. Detail lives in references/, so SKILL.md should now
// be comfortably under 200 lines.
func TestRenderSkillLineBudget(t *testing.T) {
	body := RenderSkill(RootDoc(testAppForDocs()))
	const softLimitLines = 200
	if got := strings.Count(body, "\n"); got > softLimitLines {
		t.Errorf("SKILL.md grew to %d lines (soft limit %d). Detail belongs in a topic, not the main body.", got, softLimitLines)
	}
}

// TestSkillInstallDefault: with no --target, install writes SKILL.md
// and references/ under HOME/.agents/skills/extend/.
func TestSkillInstallDefault(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	ta := newTestApp(t, newFakeServer(t, nil))
	cmd := findCmd(t, ta.app, "skill", "install")
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}

	want := filepath.Join(tmpHome, ".agents", "skills", "extend")
	assertSkillTreeInstalled(t, want)
}

// TestSkillInstallTargetPermissive checks the resolver: any of the
// three input shapes (~/.agents, ~/.agents/skills, or
// ~/.agents/skills/extend) lands at the same final directory.
func TestSkillInstallTargetPermissive(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	final := filepath.Join(tmpHome, ".agents", "skills", "extend")

	cases := []struct {
		name   string
		target string
		want   string
	}{
		{"parent-two-deep", filepath.Join(tmpHome, ".agents"), final},
		{"parent-skills", filepath.Join(tmpHome, ".agents", "skills"), final},
		{"exact-final", filepath.Join(tmpHome, ".agents", "skills", "extend"), final},
		{"non-conventional-parent", filepath.Join(tmpHome, "custom"), filepath.Join(tmpHome, "custom", "skills", "extend")},
		{"non-conventional-final", filepath.Join(tmpHome, "custom", "extend"), filepath.Join(tmpHome, "custom", "extend")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Fresh sub-home so cases don't observe each other's writes.
			_ = os.RemoveAll(tc.want)

			ta := newTestApp(t, newFakeServer(t, nil))
			cmd := findCmd(t, ta.app, "skill", "install")
			cmd.SetArgs([]string{"--target", tc.target})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("install --target %q: %v", tc.target, err)
			}
			assertSkillTreeInstalled(t, tc.want)
			if !strings.Contains(ta.errOut.String(), tc.want) {
				t.Errorf("stderr should mention the resolved target %q; got %q", tc.want, ta.errOut.String())
			}
		})
	}
}

func assertSkillTreeInstalled(t *testing.T, dir string) {
	t.Helper()
	skill, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.HasPrefix(string(skill), "---\nname: extend\n") {
		t.Errorf("SKILL.md should start with frontmatter; got %q", string(skill[:64]))
	}

	refsDir := filepath.Join(dir, "references")
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		t.Fatalf("read references dir: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	for _, want := range []string{"commands.md", "auth.md", "output.md", "lifecycle.md", "errors.md", "parse-options.md"} {
		if !got[want] {
			t.Errorf("references/%s missing under %s", want, refsDir)
		}
	}
}

// TestResolveSkillInstallDir pins the resolution matrix directly so a
// future refactor of the install command's RunE doesn't accidentally
// change which inputs resolve where.
func TestResolveSkillInstallDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty defaults to ~/.agents/skills/extend", "", filepath.Join(tmpHome, ".agents", "skills", "extend")},
		{"agents parent", filepath.Join(tmpHome, ".agents"), filepath.Join(tmpHome, ".agents", "skills", "extend")},
		{"skills parent", filepath.Join(tmpHome, ".agents", "skills"), filepath.Join(tmpHome, ".agents", "skills", "extend")},
		{"final dir as-is", filepath.Join(tmpHome, ".agents", "skills", "extend"), filepath.Join(tmpHome, ".agents", "skills", "extend")},
		{"claude parent", filepath.Join(tmpHome, ".claude"), filepath.Join(tmpHome, ".claude", "skills", "extend")},
		{"claude skills parent", filepath.Join(tmpHome, ".claude", "skills"), filepath.Join(tmpHome, ".claude", "skills", "extend")},
		{"claude final", filepath.Join(tmpHome, ".claude", "skills", "extend"), filepath.Join(tmpHome, ".claude", "skills", "extend")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSkillInstallDir(tc.input)
			if err != nil {
				t.Fatalf("resolveSkillInstallDir(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("resolveSkillInstallDir(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestSkillDescriptionUnderSpecLimit asserts the rendered description
// stays under the agentskills.io 1024-char hard limit.
func TestSkillDescriptionUnderSpecLimit(t *testing.T) {
	const specLimit = 1024
	if got := len(renderDescription()); got > specLimit {
		t.Errorf("description is %d chars, exceeds agentskills.io spec limit of %d", got, specLimit)
	}
}

// TestSkillDescriptionVerbsResolve asserts every entry in the
// descriptionVerbs table maps to a real top-level command in the doc
// tree.
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
// resourceFamilies table exists at the top level and exposes exactly
// the processorFamilyCommands set of leaves.
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

func sortStrings(s []string) {
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
// CommandDoc.Wait.DefaultsToWait.
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
			t.Errorf("verb %q has no Wait spec; skill claims it is async by default", verb)
			continue
		}
		if d.Wait.DefaultsToWait {
			t.Errorf("verb %q: Wait.DefaultsToWait = true, skill claims it is async by default", verb)
		}
	}
}

// TestSkillPaginationExampleIsValid asserts the bash example in the
// Pagination section uses a real command with real flags.
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

// TestSkillTopicLoadHintsCoverReferences ensures every topic referenced
// from writeSkillReferences has a non-empty topicLoadHint. Drift here
// means SKILL.md ships a reference entry without a load trigger.
func TestSkillTopicLoadHintsCoverReferences(t *testing.T) {
	root := RootDoc(testAppForDocs())
	body := RenderSkill(root)
	for _, fname := range extractReferenceFilenames(body) {
		name := strings.TrimSuffix(fname, ".md")
		if topicLoadHint(name) == "" {
			t.Errorf("references entry %q has no topicLoadHint", name)
		}
	}
}
