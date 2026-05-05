package grade

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// checkStableAnswer matches the agent's final message against
// substring/regex assertions in either direction. All assertions must
// pass for the expectation to pass; failures surface as evidence so a
// reader sees exactly what didn't match. All checks are
// case-insensitive — agent output varies across runs/models in casing
// more than in content.
//
//	answer_substr           — must appear (substring)
//	answer_pattern          — must match (regex)
//	answer_must_not_contain — must NOT appear (substring)
//	answer_must_not_match   — must NOT match (regex)
//
// Use answer_must_not_* for negative assertions instead of trying to
// emulate negative lookahead in answer_pattern — Go's regexp engine is
// RE2 and does not support lookaround.
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
	if exp.AnswerMustNotContain != "" {
		if strings.Contains(lo, strings.ToLower(exp.AnswerMustNotContain)) {
			return false, fmt.Sprintf("forbidden substring %q found in final message", exp.AnswerMustNotContain)
		}
	}
	if exp.AnswerMustNotMatch != "" {
		re, err := regexp.Compile("(?i)" + exp.AnswerMustNotMatch)
		if err != nil {
			return false, fmt.Sprintf("invalid regex %q: %v", exp.AnswerMustNotMatch, err)
		}
		if re.MatchString(msg) {
			return false, fmt.Sprintf("forbidden pattern %q matched", exp.AnswerMustNotMatch)
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
