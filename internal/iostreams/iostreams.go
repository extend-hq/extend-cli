// Package iostreams abstracts stdin/stdout/stderr with TTY and color detection.
// Data goes to Out, status/errors go to ErrOut, color and spinners are gated by TTY.
package iostreams

import (
	"bytes"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	stdoutTTY bool
	stderrTTY bool
	stdinTTY  bool
	colorOn   bool
}

func System() *IOStreams {
	stdoutTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	stderrTTY := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	stdinTTY := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())

	return &IOStreams{
		In:        os.Stdin,
		Out:       os.Stdout,
		ErrOut:    os.Stderr,
		stdoutTTY: stdoutTTY,
		stderrTTY: stderrTTY,
		stdinTTY:  stdinTTY,
		colorOn:   detectColor(stdoutTTY),
	}
}

func Test() (ios *IOStreams, in, out, errOut *bytes.Buffer) {
	in = &bytes.Buffer{}
	out = &bytes.Buffer{}
	errOut = &bytes.Buffer{}
	ios = &IOStreams{In: in, Out: out, ErrOut: errOut}
	return
}

func (s *IOStreams) IsStdoutTTY() bool       { return s.stdoutTTY }
func (s *IOStreams) IsStderrTTY() bool       { return s.stderrTTY }
func (s *IOStreams) IsStdinTTY() bool        { return s.stdinTTY }
func (s *IOStreams) ColorEnabled() bool      { return s.colorOn }
func (s *IOStreams) SetColorEnabled(on bool) { s.colorOn = on }
func (s *IOStreams) SetStdoutTTY(on bool)    { s.stdoutTTY = on }

// detectColor precedence:
//
//	NO_COLOR set (any value)     -> off
//	CLICOLOR=0                   -> off
//	CLICOLOR_FORCE set           -> on
//	default                      -> on iff stdout is a TTY
func detectColor(stdoutTTY bool) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if os.Getenv("CLICOLOR") == "0" {
		return false
	}
	if _, ok := os.LookupEnv("CLICOLOR_FORCE"); ok {
		return true
	}
	return stdoutTTY
}
