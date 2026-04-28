// Integration tests live in their own module to enforce a black-box boundary
// between the test code and the CLI being tested. These tests build the
// `extend` binary once at TestMain and exec it for each subtest, asserting on
// stdout/stderr/exit-code rather than importing internal types directly.
//
// Running:
//
//	cd test/integration
//	EXTEND_BASE_URL=http://localhost:3001 \
//	EXTEND_API_KEY=sk_xxx \
//	go test ./...
//
// Tests skip silently when EXTEND_BASE_URL or EXTEND_API_KEY are unset.
// Run-creating ops (extract/parse/classify/split/edit/workflow runs) cost
// credits and are gated behind EXTEND_TEST_RUN_OPS=1. Updates to existing
// shared resources are gated behind EXTEND_TEST_DESTRUCTIVE=1; do not set
// that against a production workspace.
module github.com/extend-hq/extend-cli/test/integration

go 1.26.2
