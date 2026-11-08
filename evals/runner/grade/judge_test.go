package grade

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func judgeServer(t *testing.T, response string) JudgeConfig {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	cfg := JudgeFromEnv()
	cfg.APIKey = "test-key"
	cfg.BaseURL = srv.URL
	cfg.Client = srv.Client()
	return cfg
}

// Current judge models always think, so the verdict text follows a
// thinking block.
func TestCallJudge_ReadsVerdictAfterThinking(t *testing.T) {
	cfg := judgeServer(t, `{"stop_reason":"end_turn","content":[
		{"type":"thinking","thinking":"The agent ran extend extract."},
		{"type":"text","text":"{\"passed\":true,\"evidence\":\"extend extract invoice.pdf\"}"}]}`)
	v, err := callJudge(cfg, "prompt")
	if err != nil {
		t.Fatalf("callJudge: %v", err)
	}
	if !v.Passed || v.Evidence != "extend extract invoice.pdf" {
		t.Errorf("verdict = %+v; want passed with the quoted argv", v)
	}
}

func TestCallJudge_ReportsVerdictCutOffAtMaxTokens(t *testing.T) {
	cfg := judgeServer(t, `{"stop_reason":"max_tokens","content":[
		{"type":"thinking","thinking":"..."},
		{"type":"text","text":"{\"passed\":tr"}]}`)
	_, err := callJudge(cfg, "prompt")
	if err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("err = %v; want a max_tokens cut-off error", err)
	}
}
