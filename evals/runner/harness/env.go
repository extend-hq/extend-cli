package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// baseEnv builds the environment variables every harness invocation
// receives. We deliberately scrub most of the host's environment to
// avoid leaking developer config (real API keys, custom configs) into
// the subprocess. PATH, the harness binary's needed env (HOME, USER,
// LANG), and the eval-specific overrides are propagated.
func baseEnv(opts RunOptions) []string {
	keep := []string{
		"PATH", "USER", "LANG", "LC_ALL", "TERM",
		// Required by the Anthropic and OpenAI clients respectively for
		// model-side auth.
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL",
		"OPENAI_API_KEY",
		// CODEX_API_KEY is the runtime env-var auth path consulted by
		// codex exec when there's no auth.json. See workflow comment.
		"CODEX_API_KEY",
		"CODEX_HOME",
	}
	out := []string{}
	for _, k := range keep {
		if v := os.Getenv(k); v != "" {
			out = append(out, k+"="+v)
		}
	}
	// Prepend the stub dir to PATH so `extend` resolves to our fake.
	if opts.StubDir != "" {
		out = prependPath(out, opts.StubDir)
	}
	out = append(out,
		"HOME="+opts.HomeDir,
		"EXTEND_EVAL_RECORD="+opts.RecordPath,
	)
	if opts.StubMode != "" {
		out = append(out, "EXTEND_EVAL_MODE="+opts.StubMode)
	}
	out = append(out, opts.ExtraEnv...)
	return out
}

// prependPath prepends dir to the PATH= line in env. If env doesn't
// contain a PATH= line, one is added with dir + the host's PATH as a
// safety net.
func prependPath(env []string, dir string) []string {
	for i, e := range env {
		const p = "PATH="
		if len(e) >= len(p) && e[:len(p)] == p {
			env[i] = p + dir + ":" + e[len(p):]
			return env
		}
	}
	return append(env, "PATH="+dir+":"+os.Getenv("PATH"))
}

// generateSkillTo runs `go run ./cmd/extend skill` and writes the output
// to dst. Mkdir-p the parent if needed.
func generateSkillTo(dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "run", "./cmd/extend", "skill")
	cmd.Dir = repoRoot()
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go run extend skill: %w", err)
	}
	return os.WriteFile(dst, out, 0o644)
}

// repoRoot returns the parent module's directory (extend-cli/) by
// walking up from this module's directory. Used so harness commands
// can shell into the main module to build/run the CLI and the stub.
func repoRoot() string {
	// This module lives at <repo>/evals/runner. The runner binary may
	// be invoked from anywhere; we want the parent module's root.
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cur := wd
	for {
		// The parent module's go.mod sits two levels up from us.
		parent := filepath.Dir(cur)
		if parent == cur {
			return wd
		}
		// If we hit a go.mod whose contents include the parent module
		// declaration, that's the repo root.
		mod := filepath.Join(parent, "go.mod")
		if b, err := os.ReadFile(mod); err == nil {
			if isParentModule(b) {
				return parent
			}
		}
		cur = parent
	}
}

func isParentModule(b []byte) bool {
	// Match a `module github.com/extend-hq/extend-cli` line. We don't
	// pull in golang.org/x/mod for this — a substring check is enough
	// since this is a one-time bootstrap.
	const target = "module github.com/extend-hq/extend-cli\n"
	if len(b) < len(target) {
		return false
	}
	for i := 0; i+len(target) <= len(b); i++ {
		if string(b[i:i+len(target)]) == target {
			return true
		}
	}
	return false
}
