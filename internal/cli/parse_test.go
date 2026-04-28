package cli

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParse_RawMarkdownWhenPiped(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(w, 200, map[string]any{"id": "pr_md", "status": "PENDING"})
		case http.MethodGet:
			writeJSON(w, 200, map[string]any{
				"id":     "pr_md",
				"status": "PROCESSED",
				"output": map[string]any{
					"chunks": []map[string]any{
						{"content": "# Hello\n\nWorld."},
					},
				},
			})
		}
	})
	ta := newTestApp(t, srv)

	if err := runParse(context.Background(), ta.app, parseParams{
		input:   "file_xK9",
		target:  "markdown",
		timeout: 2 * time.Second,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	got := ta.out.String()
	if !strings.Contains(got, "# Hello") || !strings.Contains(got, "World.") {
		t.Errorf("expected raw markdown, got: %q", got)
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("non-TTY output should not contain ANSI escapes, got: %q", got)
	}
}

func TestParse_JSONFormatRendersFullRun(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(w, 200, map[string]any{"id": "pr_json", "status": "PENDING"})
		case http.MethodGet:
			writeJSON(w, 200, map[string]any{
				"id":     "pr_json",
				"status": "PROCESSED",
				"output": map[string]any{"chunks": []map[string]any{{"content": "x"}}},
			})
		}
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "json"

	if err := runParse(context.Background(), ta.app, parseParams{
		input:   "file_xK9",
		target:  "markdown",
		timeout: 2 * time.Second,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	if !strings.Contains(ta.out.String(), `"chunks"`) {
		t.Errorf("expected JSON output with chunks, got: %s", ta.out.String())
	}
}

func TestParse_AsyncReturnsRunID(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_async", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "id"

	if err := runParse(context.Background(), ta.app, parseParams{
		input:  "file_xK9",
		target: "markdown",
		async:  true,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	if got := strings.TrimSpace(ta.out.String()); got != "pr_async" {
		t.Errorf("stdout = %q, want pr_async", got)
	}
}

func TestParse_ChunkingStrategyOptionsRoundTrip(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_chunked", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runParse(context.Background(), ta.app, parseParams{
		input:         "file_xK9",
		target:        "markdown",
		chunkStrategy: "section",
		chunkMinChars: 100,
		chunkMaxChars: 4000,
		async:         true,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"chunkingStrategy":{"type":"section","options":{"minCharacters":100,"maxCharacters":4000}}`) {
		t.Errorf("body should embed chunkingStrategy with options: %s", body)
	}
}

func TestParse_EngineVersionRoundTrip(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_engine", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runParse(context.Background(), ta.app, parseParams{
		input:         "file_xK9",
		target:        "markdown",
		engine:        "parse_performance",
		engineVersion: "1.0.1",
		async:         true,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := string(srv.lastRequest().Body)
	for _, want := range []string{`"engine":"parse_performance"`, `"engineVersion":"1.0.1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s: %s", want, body)
		}
	}
}

func TestParse_ChunkingStrategyNoneOmitsChunkingStrategy(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_none", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runParse(context.Background(), ta.app, parseParams{
		input:         "file_xK9",
		target:        "markdown",
		chunkStrategy: "none",
		async:         true,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if strings.Contains(body, "chunkingStrategy") {
		t.Errorf("chunk-strategy none should omit chunkingStrategy, got: %s", body)
	}
}

func TestParse_InvalidChunkingStrategyErrorsBeforeRequest(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for invalid chunk strategy")
	})
	ta := newTestApp(t, srv)
	err := runParse(context.Background(), ta.app, parseParams{
		input:         "file_xK9",
		target:        "markdown",
		chunkStrategy: "chapters",
		async:         true,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown --chunk-strategy") {
		t.Fatalf("expected chunk strategy error, got %v", err)
	}
}

func TestParse_ChunkingStrategyNoneRejectsChunkOptions(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for contradictory chunk options")
	})
	ta := newTestApp(t, srv)
	err := runParse(context.Background(), ta.app, parseParams{
		input:         "file_xK9",
		target:        "markdown",
		chunkStrategy: "none",
		chunkMaxChars: 1000,
		async:         true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be used with --chunk-strategy none") {
		t.Fatalf("expected contradictory chunk options error, got %v", err)
	}
}

func TestParse_BlockOptionsAndAdvancedOptionsFromFile(t *testing.T) {
	tmp := t.TempDir()
	bo := tmp + "/bo.json"
	ao := tmp + "/ao.json"
	if err := writeFileForTest(bo, []byte(`{"figures":{"enabled":true}}`)); err != nil {
		t.Fatal(err)
	}
	if err := writeFileForTest(ao, []byte(`{"returnOcr":{"words":true}}`)); err != nil {
		t.Fatal(err)
	}

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_opts", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runParse(context.Background(), ta.app, parseParams{
		input:               "file_xK9",
		target:              "markdown",
		blockOptionsPath:    bo,
		advancedOptionsPath: ao,
		async:               true,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"blockOptions":{"figures":{"enabled":true}}`) {
		t.Errorf("body should embed blockOptions: %s", body)
	}
	if !strings.Contains(body, `"advancedOptions":{"returnOcr":{"words":true}}`) {
		t.Errorf("body should embed advancedOptions: %s", body)
	}
}

func TestParse_BlockOptionsAndAdvancedOptionsInline(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_inline", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runParse(context.Background(), ta.app, parseParams{
		input:               "file_xK9",
		target:              "markdown",
		blockOptionsPath:    `{"tables":{"enabled":true}}`,
		advancedOptionsPath: `{"pageRanges":"1-3"}`,
		async:               true,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := string(srv.lastRequest().Body)
	for _, want := range []string{
		`"blockOptions":{"tables":{"enabled":true}}`,
		`"advancedOptions":{"pageRanges":"1-3"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s: %s", want, body)
		}
	}
}

func TestParse_AdvancedOptionsFileURI(t *testing.T) {
	tmp := t.TempDir()
	advanced := filepath.Join(tmp, "advanced options.json")
	if err := writeFileForTest(advanced, []byte(`{"pageRanges":"4-5"}`)); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_file_uri", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runParse(context.Background(), ta.app, parseParams{
		input:               "file_xK9",
		target:              "markdown",
		advancedOptionsPath: (&url.URL{Scheme: "file", Path: advanced}).String(),
		async:               true,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"advancedOptions":{"pageRanges":"4-5"}`) {
		t.Errorf("body should embed advancedOptions from file URI: %s", body)
	}
}

func TestParse_OnlyMaxCharsOmitsMin(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"id": "pr_max", "status": "PENDING"})
	})
	ta := newTestApp(t, srv)
	if err := runParse(context.Background(), ta.app, parseParams{
		input:         "file_xK9",
		target:        "markdown",
		chunkMaxChars: 8000,
		async:         true,
	}); err != nil {
		t.Fatalf("runParse: %v", err)
	}
	body := string(srv.lastRequest().Body)
	if !strings.Contains(body, `"maxCharacters":8000`) {
		t.Errorf("body should include maxCharacters: %s", body)
	}
	if strings.Contains(body, `"minCharacters"`) {
		t.Errorf("body should omit minCharacters when only --chunk-max-chars is set: %s", body)
	}
}
