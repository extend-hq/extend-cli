// The runner module is a sibling Go module (mirrors test/integration/)
// to keep its dev-time dependencies (harness drivers, JSONL parsing,
// fixture generators, eventually the LLM-judge client) out of the
// user-facing `extend` binary.
//
// Running:
//
//	cd evals/runner
//	go run ./cmd/runner ...

module github.com/extend-hq/extend-cli/evals/runner

go 1.26.2

require github.com/go-pdf/fpdf v0.9.0
