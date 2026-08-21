package iostreams

import "strings"

// SanitizeForTerminal strips escape sequences and control characters
// from server-provided text before it is printed. Anything an endpoint
// (or an intermediary rewriting its responses) controls — workspace
// names, OAuth error descriptions, API error messages — is
// attacker-influenced data; without this, embedded ANSI CSI or OSC
// sequences could restyle the terminal, spoof output, or (on some
// emulators) worse. ESC-initiated CSI and OSC sequences are removed
// whole, including their payload; all other C0 controls, DEL, and the
// raw C1 range (which includes single-byte CSI) are dropped. These are
// single-line values, so newlines carry no meaning here and are
// dropped with the rest.
func SanitizeForTerminal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1b { // ESC
			if i+1 >= len(runes) {
				break
			}
			switch runes[i+1] {
			case '[': // CSI: parameters end at a byte in 0x40–0x7e
				i++
				for i+1 < len(runes) {
					i++
					if runes[i] >= 0x40 && runes[i] <= 0x7e {
						break
					}
				}
			case ']': // OSC: terminated by BEL or ST (ESC \)
				i++
				for i+1 < len(runes) {
					i++
					if runes[i] == 0x07 {
						break
					}
					if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '\\' {
						i++
						break
					}
				}
			default: // two-character escape (RIS, charset shifts, ...)
				i++
			}
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
