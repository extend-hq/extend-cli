package grade

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// LegitimateIDs is the set of IDs the agent can legitimately discover
// from `*list*` invocations against the stub. Any ID matching a
// must_not_fabricate_ids pattern that is NOT in this set is flagged
// as fabrication.
//
// Source of truth: evals/stub/fixtures.go. Keep in sync; the runner
// test in fabrication_test.go enforces the match.
var LegitimateIDs = map[string]bool{
	// extractors
	"ex_invoiceQ3": true,
	"ex_receipt01": true,
	// classifiers
	"cl_contracts01": true,
	// splitters
	"spl_statements01": true,
	// workflows
	"workflow_invoice": true,
	// files
	"file_inv001": true,
	"file_inv002": true,
	// runs
	"exr_demo_processed": true,
	"exr_demo_failed":    true,
	"exr_failed_002":     true,
	"exr_failed_003":     true,
	"exr_failed_004":     true,
	"exr_failed_005":     true,
	"pr_demo_processed":  true,
}

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
	`webhook_[a-zA-Z0-9_-]+`,
	`webhook_subscription_[a-zA-Z0-9_-]+`,
}

// checkFabrication scans every recorded `extend` invocation's argv for
// ID-shaped tokens matching the configured patterns. Any token not in
// the legitimate set (and not in AllowedExtraIDs) counts as a
// fabrication.
//
// Special case: "create" calls legitimately produce new IDs that the
// agent then uses in subsequent calls. We track those by parsing the
// stub's responses... but Phase 1 doesn't capture stub stdout. So
// for Phase 1 we treat any post-create ID matching the patterns as
// suspect-but-allowed if it's the FIRST appearance after a create call
// of that resource family. Simpler: for Phase 1, exclude calls
// invoked after any matching `create` from this check entirely.
func checkFabrication(exp spec.Expectation, in Inputs) (bool, string) {
	patterns := exp.Patterns
	if len(patterns) == 0 {
		patterns = DefaultFabricationPatterns
	}
	allowedExtra := map[string]bool{}
	for _, id := range exp.AllowedExtraIDs {
		allowedExtra[id] = true
	}

	res := []*regexp.Regexp{}
	for _, p := range patterns {
		re, err := regexp.Compile(`\b` + p + `\b`)
		if err != nil {
			return false, fmt.Sprintf("invalid pattern %q: %v", p, err)
		}
		res = append(res, re)
	}

	fabricated := map[string]bool{}
	createdFamilies := map[string]bool{}

	for _, c := range in.Calls {
		// Track whether this call is a `<resource> create` — anything
		// after a create can plausibly use the returned ID. For Phase 1
		// we use this as a permissive escape hatch.
		pos := positional(c.Argv)
		if len(pos) >= 2 && pos[1] == "create" {
			createdFamilies[pos[0]] = true
		}

		for _, tok := range c.Argv {
			for _, re := range res {
				for _, hit := range re.FindAllString(tok, -1) {
					if LegitimateIDs[hit] || allowedExtra[hit] {
						continue
					}
					if isFromCreatedFamily(hit, createdFamilies) {
						continue
					}
					fabricated[hit] = true
				}
			}
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

// isFromCreatedFamily returns true if id's prefix matches a resource
// family the agent created earlier in the session.
func isFromCreatedFamily(id string, families map[string]bool) bool {
	// Map id prefix -> resource family the user creates via the CLI.
	prefix := strings.SplitN(id, "_", 2)[0] + "_"
	switch prefix {
	case "ex_":
		return families["extractors"]
	case "exv_":
		return families["extractors"]
	case "cl_":
		return families["classifiers"]
	case "spl_":
		return families["splitters"]
	case "workflow_":
		return families["workflows"]
	case "evs_":
		return families["evaluations"]
	case "webhook_":
		return families["webhooks"]
	case "webhook_subscription_":
		return families["webhooks"]
	case "file_":
		return families["files"]
	}
	return false
}
