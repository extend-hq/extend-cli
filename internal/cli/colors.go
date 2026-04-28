package cli

import (
	"fmt"

	"github.com/extend-hq/extend-cli/internal/iostreams"
)

const (
	ansiGreen  = "\033[32m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiDim    = "\033[2m"
	ansiReset  = "\033[0m"
)

type colorFn func(string) string

type palette struct {
	enabled bool
}

func paletteFor(io *iostreams.IOStreams) palette {
	return palette{enabled: io.ColorEnabled()}
}

func (p palette) wrap(code, s string) string {
	if !p.enabled {
		return s
	}
	return code + s + ansiReset
}

func (p palette) Green(s string) string  { return p.wrap(ansiGreen, s) }
func (p palette) Red(s string) string    { return p.wrap(ansiRed, s) }
func (p palette) Yellow(s string) string { return p.wrap(ansiYellow, s) }
func (p palette) Cyan(s string) string   { return p.wrap(ansiCyan, s) }
func (p palette) Dim(s string) string    { return p.wrap(ansiDim, s) }

func (p palette) Greenf(format string, args ...any) string {
	return p.Green(fmt.Sprintf(format, args...))
}
func (p palette) Redf(format string, args ...any) string { return p.Red(fmt.Sprintf(format, args...)) }
func (p palette) Dimf(format string, args ...any) string { return p.Dim(fmt.Sprintf(format, args...)) }
