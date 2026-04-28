package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// extendBinary is the path to the CLI binary built once by TestMain. All
// subtests exec this path rather than re-building per test.
var extendBinary string

// TestMain compiles the CLI binary once. We deliberately do this in the
// integration module's TestMain (rather than relying on the consumer to set
// `EXTEND_BIN`) so the test harness fails loudly if the build is broken
// instead of silently exec'ing a stale or missing binary.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "extend-itest-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: mkdtemp: %v\n", err)
		os.Exit(2)
	}
	defer os.RemoveAll(tmp)

	bin := filepath.Join(tmp, "extend")
	// `go build ./cmd/extend` must be invoked from the parent module's root,
	// not from this test module — the cmd/extend package isn't reachable
	// from this module's import graph.
	build := exec.Command("go", "build", "-o", bin, "./cmd/extend")
	build.Dir = "../.."
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: go build failed: %v\n", err)
		os.Exit(2)
	}
	extendBinary = bin

	os.Exit(m.Run())
}

// envSetup carries the live-API connection details for one test. Use
// requireEnv(t) to fetch it; the helper skips the test when configuration
// is missing so a developer can `go test ./...` without setting anything.
type envSetup struct {
	BaseURL string
	APIKey  string
}

// requireEnv returns the API connection details, skipping the test if either
// EXTEND_BASE_URL or EXTEND_API_KEY is unset. Both must be set explicitly —
// we never default to production to avoid accidental credit/data mutation.
func requireEnv(t *testing.T) envSetup {
	t.Helper()
	base := os.Getenv("EXTEND_BASE_URL")
	if base == "" {
		t.Skip("EXTEND_BASE_URL not set; skipping integration test")
	}
	key := os.Getenv("EXTEND_API_KEY")
	if key == "" {
		t.Skip("EXTEND_API_KEY not set; skipping integration test")
	}
	return envSetup{BaseURL: base, APIKey: key}
}

// requireRunOps gates tests that create runs (extract/parse/classify/split/
// edit/workflow). Each of these costs API credits, so we require an explicit
// opt-in via EXTEND_TEST_RUN_OPS=1 to keep `go test ./...` against a prod
// workspace cheap by default.
func requireRunOps(t *testing.T) {
	t.Helper()
	if os.Getenv("EXTEND_TEST_RUN_OPS") != "1" {
		t.Skip("EXTEND_TEST_RUN_OPS != 1; skipping run-creating test (these cost credits)")
	}
}

// requireDestructive gates tests that mutate shared/long-lived resources.
// Set EXTEND_TEST_DESTRUCTIVE=1 only against ephemeral test environments.
func requireDestructive(t *testing.T) {
	t.Helper()
	if os.Getenv("EXTEND_TEST_DESTRUCTIVE") != "1" {
		t.Skip("EXTEND_TEST_DESTRUCTIVE != 1; skipping destructive update test")
	}
}

// itestName generates a resource name unique to one test invocation.
//
// The format is `cli-itest-<sanitized-test>-<unix-ms>`. The sanitized test
// name strips characters disallowed in some Extend resource names (notably
// "/" used by Go subtests) and truncates so the full name stays under the
// API's 100-char limit. The unix-ms suffix prevents collisions across
// rapid-succession runs.
func itestName(t *testing.T) string {
	t.Helper()
	clean := strings.NewReplacer("/", "-", " ", "-", "_", "-").Replace(t.Name())
	const maxBase = 60
	if len(clean) > maxBase {
		clean = clean[:maxBase]
	}
	return fmt.Sprintf("cli-itest-%s-%d", clean, time.Now().UnixMilli())
}

// extendResult captures one CLI invocation. All fields are populated even
// when ExitCode != 0; failing tests can read Stderr to surface the server's
// error message in test output.
type extendResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// runExtend executes the built CLI binary with the given args, with
// EXTEND_BASE_URL / EXTEND_API_KEY injected from the env captured by
// requireEnv. The returned struct never panics on non-zero exit; tests must
// inspect ExitCode explicitly when they expect failure.
func runExtend(t *testing.T, env envSetup, args ...string) extendResult {
	t.Helper()
	return runExtendWithStdin(t, env, nil, args...)
}

func runExtendWithStdin(t *testing.T, env envSetup, stdin []byte, args ...string) extendResult {
	t.Helper()
	cmd := exec.Command(extendBinary, args...)
	cmd.Env = append(os.Environ(),
		"EXTEND_BASE_URL="+env.BaseURL,
		"EXTEND_API_KEY="+env.APIKey,
		// Disable color to make stdout assertions stable.
		"NO_COLOR=1",
		// Force non-TTY output so the CLI emits JSON for `-o json` paths
		// instead of any TTY-only summary rendering.
		"TERM=dumb",
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	res := extendResult{Stdout: out.Bytes(), Stderr: errOut.Bytes()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("runExtend(%v): exec failed: %v\nstderr=%s", args, err, errOut.String())
	}
	return res
}

// requireOK fails the test if the CLI exited non-zero, dumping stderr.
func (r extendResult) requireOK(t *testing.T, args ...string) {
	t.Helper()
	if r.ExitCode != 0 {
		t.Fatalf("extend %v exited %d\nstdout: %s\nstderr: %s",
			args, r.ExitCode, r.Stdout, r.Stderr)
	}
}

// decodeJSON decodes the CLI's stdout (assumed to be `-o json` output) into
// the target. Fails the test on any decode error.
func (r extendResult) decodeJSON(t *testing.T, target any) {
	t.Helper()
	if err := json.Unmarshal(r.Stdout, target); err != nil {
		t.Fatalf("decode stdout JSON: %v\nstdout: %s", err, r.Stdout)
	}
}

// rememberCleanup registers a deferred CLI cleanup invocation that runs
// during t.Cleanup. It logs (rather than fails) if cleanup itself errors so
// a single leaked resource doesn't mask the actual test failure.
//
// `op` is the human-readable verb logged on cleanup error (e.g. "delete
// extractor"). `args` are passed verbatim to the CLI.
func rememberCleanup(t *testing.T, env envSetup, op string, args ...string) {
	t.Helper()
	t.Cleanup(func() {
		res := runExtend(t, env, args...)
		if res.ExitCode != 0 {
			t.Logf("integration cleanup (%s) failed: extend %v exited %d\nstderr: %s",
				op, args, res.ExitCode, res.Stderr)
		}
	})
}

// once is exposed as a small convenience for any test that needs lazy init.
// Currently unused; kept here so tests can reach for it without redefining
// the same pattern repeatedly.
var _ = sync.Once{}
