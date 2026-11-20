package harness

import "testing"

func TestClaudeModel_ReadsInitEvent(t *testing.T) {
	events := []map[string]any{
		{"type": "system", "subtype": "hook_started"},
		{"type": "system", "subtype": "init", "model": "claude-opus-5-5"},
		{"type": "assistant", "message": map[string]any{"model": "claude-haiku-5-5"}},
	}
	if got := claudeModel(events); got != "claude-opus-5-5" {
		t.Errorf("claudeModel = %q; want claude-opus-5-5", got)
	}
	if got := claudeModel(nil); got != "" {
		t.Errorf("claudeModel(nil) = %q; want empty", got)
	}
}
