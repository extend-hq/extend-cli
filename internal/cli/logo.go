package cli

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// extendLogoPNG is the official Extend wordmark+mark, embedded so the
// setup wizard can render the real brand as terminal half-block art
// rather than approximating it with hand-drawn glyphs. Kept in sync with
// the repo-root extend-logo.png.
//
//go:embed extend-logo.png
var extendLogoPNG []byte

var (
	logoImgOnce sync.Once
	logoImg     image.Image
	logoImgErr  error
)

func decodeLogo() (image.Image, error) {
	logoImgOnce.Do(func() {
		logoImg, _, logoImgErr = image.Decode(bytes.NewReader(extendLogoPNG))
	})
	return logoImg, logoImgErr
}

// logoCell is one character cell of the rendered logo. Two vertical
// source pixels pack into a single cell via the half-block glyphs
// (█ ▀ ▄), so one text row covers two pixel rows.
type logoCell struct {
	glyph string
	lit   bool
}

// buildLogoGrid samples the embedded logo into a grid of half-block cells
// at the requested cell width, preserving aspect ratio. Dark pixels (the
// logo strokes) are "lit"; the near-white background is blank. Returns
// nil if the image can't be decoded so callers can fall back to a text
// title.
func buildLogoGrid(cols int) [][]logoCell {
	if cols < 8 {
		cols = 8
	}
	img, err := decodeLogo()
	if err != nil || img == nil {
		return nil
	}
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw == 0 || ih == 0 {
		return nil
	}
	// Terminal cells are ~twice as tall as wide; half-blocks restore
	// vertical resolution, so sample a pixel grid of cols × pxRows where
	// pxRows tracks the true aspect ratio.
	pxRows := cols * ih / iw
	if pxRows < 2 {
		pxRows = 2
	}
	if pxRows%2 == 1 {
		pxRows++
	}
	lit := func(px, py int) bool {
		sx := b.Min.X + px*iw/cols
		sy := b.Min.Y + py*ih/pxRows
		r, g, bl, a := img.At(sx, sy).RGBA()
		if a < 0x8000 {
			return false
		}
		lum := (299*r + 587*g + 114*bl) / 1000
		return lum < 0x8000
	}
	rows := make([][]logoCell, 0, pxRows/2)
	for py := 0; py < pxRows; py += 2 {
		row := make([]logoCell, cols)
		for px := 0; px < cols; px++ {
			top := lit(px, py)
			bot := py+1 < pxRows && lit(px, py+1)
			var g string
			switch {
			case top && bot:
				g = "█"
			case top:
				g = "▀"
			case bot:
				g = "▄"
			default:
				g = " "
			}
			row[px] = logoCell{glyph: g, lit: top || bot}
		}
		rows = append(rows, row)
	}
	return rows
}

// Logo shimmer endpoints: a vivid indigo base with a bright cyan
// highlight band sweeping across it.
var (
	logoBase      = [3]int{0x3B, 0x82, 0xF6}
	logoHighlight = [3]int{0xBF, 0xDB, 0xFE}
)

const logoGradientStops = 24

// buildLogoStyles precomputes the gradient between base and highlight so
// the animated renderer indexes into a small fixed palette instead of
// allocating a lipgloss style per cell per frame.
func buildLogoStyles() []lipgloss.Style {
	styles := make([]lipgloss.Style, logoGradientStops)
	for i := range styles {
		t := float64(i) / float64(logoGradientStops-1)
		styles[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(lerpHex(logoBase, logoHighlight, t)))
	}
	return styles
}

func lerpHex(a, b [3]int, t float64) string {
	r := int(float64(a[0]) + float64(b[0]-a[0])*t)
	g := int(float64(a[1]) + float64(b[1]-a[1])*t)
	bl := int(float64(a[2]) + float64(b[2]-a[2])*t)
	return fmt.Sprintf("#%02X%02X%02X", clamp8(r), clamp8(g), clamp8(bl))
}

func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// logoAnim renders an animated frame of the logo grid.
type logoAnim struct {
	grid    [][]logoCell
	cols    int
	styles  []lipgloss.Style
	colorOn bool
}

// render draws the logo for the given animation frame. revealCols clips
// the logo to its left-most columns for the intro wipe (pass cols for the
// fully-revealed logo). A bright highlight band sweeps across the lit
// cells to give the brand a subtle shimmer.
func (l logoAnim) render(frame, revealCols int) string {
	if len(l.grid) == 0 {
		return ""
	}
	n := len(l.styles)
	bandW := float64(l.cols) / 4.0
	if bandW < 4 {
		bandW = 4
	}
	// Highlight centre sweeps left→right, looping with a gap so the
	// shine restarts cleanly off-screen.
	span := float64(l.cols) + 2*bandW
	center := math.Mod(float64(frame)*1.6, span) - bandW

	var b strings.Builder
	for _, row := range l.grid {
		for x, cell := range row {
			if x >= revealCols {
				b.WriteByte(' ')
				continue
			}
			if !cell.lit {
				b.WriteByte(' ')
				continue
			}
			if !l.colorOn || n == 0 {
				b.WriteString(cell.glyph)
				continue
			}
			d := math.Abs(float64(x) - center)
			t := 1.0 - d/bandW
			if t < 0 {
				t = 0
			}
			idx := int(t * float64(n-1))
			if idx < 0 {
				idx = 0
			} else if idx >= n {
				idx = n - 1
			}
			b.WriteString(l.styles[idx].Render(cell.glyph))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
