package grade

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// DefaultFabricationPatterns covers every Extend ID prefix the CLI
// recognizes. Adding a new ID-prefixed entity to the CLI should add
// its pattern here.
var DefaultFabricationPatterns = []string{
	`ex_[a-zA-Z0-9_-]+`,
	`exv_[a-zA-Z0-9_-]+`,
	`exr_[a-zA-Z0-9_-]+`,
	`cl_[a-zA-Z0-9_-]+`,
	`clr_[a-zA-Z0-9_-]+`,
	`spl_[a-zA-Z0-9_-]+`,
	`splr_[a-zA-Z0-9_-]+`,
	`pr_[a-zA-Z0-9_-]+`,
	`edr_[a-zA-Z0-9_-]+`,
	`workflow_[a-zA-Z0-9_-]+`,
	`workflow_run_[a-zA-Z0-9_-]+`,
	`file_[a-zA-Z0-9_-]+`,
	`evs_[a-zA-Z0-9_-]+`,
	`esr_[a-zA-Z0-9_-]+`,
	`evr_[a-zA-Z0-9_-]+`,
	`webhook_[a-zA-Z0-9_-]+`,
	`webhook_subscription_[a-zA-Z0-9_-]+`,
}

// checkFabrication scans every recorded `extend` invocation's argv for
// ID-shaped tokens matching the configured patterns. Any token that
// did NOT appear in a prior stub response (stdout/stderr) is treated
// as a fabrication.
//
// The escape valve is the prompt itself: any ID the user explicitly
// included in the prompt (e.g. `Watch run exr_demo_processed` for S-3)
// is bootstrapped into the legitimate set. Otherwise a Path-B prompt
// containing IDs would always look like fabrication on the first call
// before any stub response had returned them.
func checkFabrication(exp spec.Expectation, in Inputs) (bool, string) {
	patterns := exp.Patterns
	if len(patterns) == 0 {
		patterns = DefaultFabricationPatterns
	}
	allowedExtra := map[string]bool{}
	for _, id := range exp.AllowedExtraIDs {
		allowedExtra[id] = true
	}

	res := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(`\b` + p + `\b`)
		if err != nil {
			return false, fmt.Sprintf("invalid pattern %q: %v", p, err)
		}
		res = append(res, re)
	}

	// Bootstrap legitimate IDs from the prompt — any ID-shaped token
	// the user mentioned is fair game from the first call.
	legit := extractIDs(in.Eval.Prompt, res)

	// Walk calls in order: each call's argv is checked against the
	// legitimate set built from prior calls' responses (and the
	// prompt). Then this call's response IDs are added to the set
	// for subsequent calls.
	fabricated := map[string]bool{}
	for _, c := range in.Calls {
		for _, tok := range c.Argv {
			for _, hit := range scanIDs(tok, res) {
				if legit[hit] || allowedExtra[hit] {
					continue
				}
				fabricated[hit] = true
			}
		}
		// After checking, harvest IDs the stub returned in this call's
		// response and add them to the legitimate set for the next.
		for _, hit := range scanIDs(c.Stdout, res) {
			legit[hit] = true
		}
		for _, hit := range scanIDs(c.Stderr, res) {
			legit[hit] = true
		}
	}

	if len(fabricated) == 0 {
		return true, fmt.Sprintf("no fabricated IDs across %d call(s)", len(in.Calls))
	}
	keys := make([]string, 0, len(fabricated))
	for k := range fabricated {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return false, "fabricated: " + strings.Join(keys, ", ")
}

// extractIDs returns the set of IDs in s matching any of the patterns.
func extractIDs(s string, patterns []*regexp.Regexp) map[string]bool {
	out := map[string]bool{}
	for _, hit := range scanIDs(s, patterns) {
		out[hit] = true
	}
	return out
}

func scanIDs(s string, patterns []*regexp.Regexp) []string {
	var out []string
	for _, re := range patterns {
		out = append(out, re.FindAllString(s, -1)...)
	}
	return out
}
