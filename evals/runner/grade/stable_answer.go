package grade

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// checkStableAnswer matches the agent's final message against a
// substring or regex. Both checks are case-insensitive — agent output
// varies across runs/models in casing more than in content.
//
// Either AnswerSubstr or AnswerPattern (or both) may be set; both must
// match if both are present.
func checkStableAnswer(exp spec.Expectation, in Inputs) (bool, string) {
	if in.Harness == nil || in.Harness.FinalMessage == "" {
		return false, "no final agent message captured"
	}
	msg := in.Harness.FinalMessage
	lo := strings.ToLower(msg)

	if exp.AnswerSubstr != "" {
		if !strings.Contains(lo, strings.ToLower(exp.AnswerSubstr)) {
			return false, fmt.Sprintf("substring %q not found in final message", exp.AnswerSubstr)
		}
	}
	if exp.AnswerPattern != "" {
		re, err := regexp.Compile("(?i)" + exp.AnswerPattern)
		if err != nil {
			return false, fmt.Sprintf("invalid regex %q: %v", exp.AnswerPattern, err)
		}
		if !re.MatchString(msg) {
			return false, fmt.Sprintf("pattern %q not matched", exp.AnswerPattern)
		}
	}

	return true, snippet(msg, 120)
}

// snippet returns the first n chars of s with whitespace collapsed,
// suitable for embedding in evidence strings.
func snippet(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return "“" + s + "”"
	}
	return "“" + s[:n] + "…”"
}
