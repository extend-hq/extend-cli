// Package spec defines the on-disk schema of evals.json. The format
// follows the skill-creator reference implementation conventions
// (`expectations` rather than the agentskills.io spec doc's `assertions`)
// so artifacts can interoperate with that ecosystem if needed.
//
// Stability: this schema is part of the project's public surface for
// the evals workflow. Breaking changes require a version bump.
package spec

import (
	"encoding/json"
	"fmt"
	"os"
)

// File is the top-level evals.json shape.
type File struct {
	SkillName string `json:"skill_name"`
	Evals     []Eval `json:"evals"`
}

// Eval is one test case.
type Eval struct {
	ID             string        `json:"id"`
	Category       string        `json:"category"`
	Path           Path          `json:"path"`
	Prompt         string        `json:"prompt"`
	Files          []string      `json:"files,omitempty"`
	ExpectedOutput string        `json:"expected_output,omitempty"`
	Modes          []Mode        `json:"modes"`
	StubConfig     StubConfig    `json:"stub_config,omitempty"`
	Expectations   []Expectation `json:"expectations"`
	Notes          string        `json:"notes,omitempty"`
}

// Path is "A" (natural-phrasing) or "B" (explicit-context). See
// evals/AGENTS.md for the discriminator.
type Path string

const (
	PathA Path = "A"
	PathB Path = "B"
)

// Mode is "trigger" or "outcome". A case can opt into either or both.
//
//	trigger — early-terminate as soon as the agent uses any extend-related
//	          tool; cheaper, used for trigger-discrimination cases.
//	outcome — let the agent run the task to completion; capture every
//	          extend call and the final answer; used for command-shape
//	          and workflow cases.
type Mode string

const (
	ModeTrigger Mode = "trigger"
	ModeOutcome Mode = "outcome"
)

// StubConfig configures the fake `extend` binary's behaviour for one
// case. Fields default to zero/empty when omitted.
type StubConfig struct {
	// DefaultMode selects the stub's response strategy for unmatched
	// commands. One of "real_responses" (default), "paginated",
	// "auth_error".
	DefaultMode string `json:"default_mode,omitempty"`

	// Pages is the number of pages the stub should split list responses
	// across when DefaultMode == "paginated". Defaults to 2.
	Pages int `json:"pages,omitempty"`
}

// Expectation is a tagged union; the active fields depend on Type.
//
// Marshalling: this is encoded as a flat object with a "type"
// discriminator, not a nested wrapper. Unused fields stay zero/empty.
type Expectation struct {
	Type ExpectationType `json:"type"`

	// Common: a short label used in reports.
	Text string `json:"text,omitempty"`

	// skill_activates
	Skill string `json:"skill,omitempty"`

	// extend_call
	MustContain    []ExtendCallPredicate `json:"must_contain,omitempty"`
	MustNotContain []ExtendCallPredicate `json:"must_not_contain,omitempty"`
	CountUnder     map[string]int        `json:"count_under,omitempty"`    // verb path -> max count
	CountAtLeast   map[string]int        `json:"count_at_least,omitempty"` // verb path -> min count

	// stable_answer
	Criterion            string `json:"criterion,omitempty"`
	AnswerPattern        string `json:"answer_pattern,omitempty"`          // regex; case-insensitive
	AnswerSubstr         string `json:"answer_substr,omitempty"`           // substring; case-insensitive
	AnswerMustNotContain string `json:"answer_must_not_contain,omitempty"` // substring; case-insensitive
	AnswerMustNotMatch   string `json:"answer_must_not_match,omitempty"`   // regex; case-insensitive

	// must_not_fabricate_ids
	Patterns        []string `json:"patterns,omitempty"`          // regex patterns of ID shapes to ban
	AllowedExtraIDs []string `json:"allowed_extra_ids,omitempty"` // explicit IDs the agent may fabricate (rare)
}

// ExpectationType enum.
type ExpectationType string

const (
	TypeSkillActivates      ExpectationType = "skill_activates"
	TypeExtendCall          ExpectationType = "extend_call"
	TypeStableAnswer        ExpectationType = "stable_answer"
	TypeMustNotFabricateIDs ExpectationType = "must_not_fabricate_ids"
	TypeJudge               ExpectationType = "judge"
)

// ExtendCallPredicate matches against one recorded `extend` invocation.
// All non-empty fields must match for the predicate to fire.
type ExtendCallPredicate struct {
	// ArgvPrefix is the leading positional verb sequence to match
	// (e.g. ["extract"], ["runs", "list"]). Flags are skipped during
	// the prefix check.
	ArgvPrefix []string `json:"argv_prefix,omitempty"`

	// Args lists strings that must appear anywhere in the argv (after
	// the prefix). Use this for required flag values like
	// ["--using", "ex_abc"].
	Args []string `json:"args,omitempty"`

	// Flag, if set, requires the named long flag to be present.
	Flag string `json:"flag,omitempty"`

	// FlagValue, if set, requires the named flag to have the given
	// value (matches both `--flag=val` and `--flag val`).
	FlagValue map[string]string `json:"flag_value,omitempty"`

	// MustNotHaveFlag requires the named long flag to NOT be present.
	MustNotHaveFlag string `json:"must_not_have_flag,omitempty"`
}

// Load reads and parses evals.json from disk.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}
	return &f, nil
}

// Validate enforces shape rules at load time so authoring mistakes
// surface before any harness is spawned.
func (f *File) Validate() error {
	if f.SkillName == "" {
		return fmt.Errorf("skill_name is required")
	}
	seen := map[string]bool{}
	for i, e := range f.Evals {
		if e.ID == "" {
			return fmt.Errorf("evals[%d]: id is required", i)
		}
		if seen[e.ID] {
			return fmt.Errorf("evals[%d]: duplicate id %q", i, e.ID)
		}
		seen[e.ID] = true
		if e.Path != PathA && e.Path != PathB {
			return fmt.Errorf("eval %s: path must be 'A' or 'B' (got %q)", e.ID, e.Path)
		}
		if e.Prompt == "" {
			return fmt.Errorf("eval %s: prompt is required", e.ID)
		}
		if len(e.Modes) == 0 {
			return fmt.Errorf("eval %s: at least one mode required", e.ID)
		}
		for _, m := range e.Modes {
			if m != ModeTrigger && m != ModeOutcome {
				return fmt.Errorf("eval %s: invalid mode %q", e.ID, m)
			}
		}
		if len(e.Expectations) == 0 {
			return fmt.Errorf("eval %s: at least one expectation required", e.ID)
		}
		for j, exp := range e.Expectations {
			if err := exp.Validate(); err != nil {
				return fmt.Errorf("eval %s, expectation %d: %w", e.ID, j, err)
			}
		}
	}
	return nil
}

// Validate enforces per-expectation shape: required fields per type.
func (e *Expectation) Validate() error {
	switch e.Type {
	case TypeSkillActivates:
		if e.Skill == "" {
			return fmt.Errorf("skill_activates: skill is required")
		}
	case TypeExtendCall:
		if len(e.MustContain) == 0 && len(e.MustNotContain) == 0 &&
			len(e.CountUnder) == 0 && len(e.CountAtLeast) == 0 {
			return fmt.Errorf("extend_call: at least one of must_contain/must_not_contain/count_under/count_at_least required")
		}
	case TypeStableAnswer:
		if e.AnswerPattern == "" && e.AnswerSubstr == "" &&
			e.AnswerMustNotContain == "" && e.AnswerMustNotMatch == "" {
			return fmt.Errorf("stable_answer: answer_pattern, answer_substr, answer_must_not_contain, or answer_must_not_match required")
		}
	case TypeMustNotFabricateIDs:
		if len(e.Patterns) == 0 {
			return fmt.Errorf("must_not_fabricate_ids: patterns required")
		}
	case TypeJudge:
		if e.Criterion == "" {
			return fmt.Errorf("judge: criterion required")
		}
	default:
		return fmt.Errorf("unknown expectation type %q", e.Type)
	}
	return nil
}
