package grade

import (
	"fmt"

	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// checkSkillActivates passes if the harness saw any evidence of skill
// activation: SKILL.md was read, or the agent invoked our stub
// `extend` binary at least once.
func checkSkillActivates(exp spec.Expectation, in Inputs) (bool, string) {
	if in.Harness != nil && in.Harness.SkillRead {
		return true, "harness recorded SKILL.md read or skill tool use"
	}
	if len(in.Calls) > 0 {
		return true, fmt.Sprintf("agent invoked extend %d time(s)", len(in.Calls))
	}
	return false, "no SKILL.md read and no extend invocations"
}
