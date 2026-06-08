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

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
)

// extendWordmarkPNG is the brand "extend" wordmark, rasterized at runtime
// into braille. The setup wizard composes this with a parametric two-
// diamond mark drawn alongside it.
//
//go:embed extend-wordmark.png
var extendWordmarkPNG []byte

// logoWordmark is the text the wordmark spells, used to label each
// detected letter region (regions[1..N]) with its ASCII character.
const logoWordmark = "extend"

var (
	wordmarkImgOnce sync.Once
	wordmarkImg     image.Image
	wordmarkImgErr  error
)

func decodeWordmark() (image.Image, error) {
	wordmarkImgOnce.Do(func() {
		wordmarkImg, _, wordmarkImgErr = image.Decode(bytes.NewReader(extendWordmarkPNG))
	})
	return wordmarkImg, wordmarkImgErr
}

// Base mark geometry in braille dots (scale=1). All mark dimensions are
// scaled by `(markCols*2)/logoMarkCanvasW` so the mark and the wordmark
// grow/shrink together with the requested lockup width. Same proportions
// as cmd/anim-demo.
const (
	logoMarkCanvasW      = 40
	logoMarkCanvasH      = 64
	logoMarkRX           = 16.0
	logoMarkRY           = 9.0
	logoMarkInnerRxRatio = 0.75
	logoMarkInnerRyRatio = 0.67
	logoMarkCornerH      = 1.5
	logoMarkTightGap     = 6.0
	logoLockupPadding    = 3 // empty cols between mark and wordmark
)

// logoMarkWidthFrac is the mark's share of the non-padding lockup width,
// derived from the brand asset: the SVG viewBox is 129×24 with the mark
// occupying ~32.5 of that width, the wordmark ~84, so mark/total ≈ 0.279.
const logoMarkWidthFrac = 0.279

// logoWordmarkShrink scales the wordmark down a touch from its strict
// proportional share so the text reads visually a bit smaller than the
// mark (the brand wordmark has descenders that make it taller than the
// mark; in the terminal we soften that).
const logoWordmarkShrink = 0.82

const logoMinWordmarkW = 16 // below this the wordmark is hidden

// logoCell is one character cell of the rendered logo. `glyph` is either
// a braille char or a space; `lit` flags cells that should receive the
// shimmer tint.
type logoCell struct {
	glyph string
	lit   bool
}

// dotGrid is a binary 2D buffer of braille subpixels (2 dots wide × 4
// dots tall per char cell). Used as a scratch buffer that's later packed
// into [][]logoCell.
type dotGrid struct {
	w, h int
	dots []bool
}

func newDotGrid(w, h int) *dotGrid {
	if w%2 != 0 {
		w++
	}
	for h%4 != 0 {
		h++
	}
	return &dotGrid{w: w, h: h, dots: make([]bool, w*h)}
}

func (g *dotGrid) set(x, y int) {
	if x >= 0 && x < g.w && y >= 0 && y < g.h {
		g.dots[y*g.w+x] = true
	}
}

// fillBluntDiamondRing paints the annular region between an outer
// blunted-diamond (pointed top/bottom, rounded left/right) and an inner
// sharp rhombus.
func (g *dotGrid) fillBluntDiamondRing(cx, cy, rxOuter, ryOuter, rxInner, ryInner, cornerH float64) {
	yMin := int(math.Floor(cy - ryOuter))
	yMax := int(math.Ceil(cy + ryOuter))
	xMin := int(math.Floor(cx - rxOuter))
	xMax := int(math.Ceil(cx + rxOuter))
	for y := yMin; y <= yMax; y++ {
		for x := xMin; x <= xMax; x++ {
			dx := math.Abs(float64(x) - cx)
			dy := math.Abs(float64(y) - cy)
			if dy > ryOuter {
				continue
			}
			var outerX float64
			if dy <= cornerH {
				outerX = rxOuter
			} else {
				outerX = rxOuter * (ryOuter - dy) / (ryOuter - cornerH)
			}
			if dx > outerX {
				continue
			}
			if dx/rxInner+dy/ryInner < 1.0 {
				continue
			}
			g.set(x, y)
		}
	}
}

// markDims are the per-render mark dimensions, scaled to fit a requested
// character width.
type markDims struct {
	canvasW  int
	canvasH  int
	centerX  float64
	centerY  float64
	rx       float64
	ry       float64
	cornerH  float64
	tightGap float64
}

// markDimsFor returns mark dimensions scaled so the rendered mark is
// roughly `cols` characters wide. All linear measurements (rx, ry,
// cornerH, tightGap) scale by the same factor, preserving the brand
// proportions.
func markDimsFor(cols int) markDims {
	if cols < 6 {
		cols = 6
	}
	canvasW := cols * 2
	if canvasW%2 != 0 {
		canvasW++
	}
	scale := float64(canvasW) / float64(logoMarkCanvasW)
	canvasH := int(math.Round(float64(logoMarkCanvasH) * scale))
	for canvasH%4 != 0 {
		canvasH++
	}
	return markDims{
		canvasW:  canvasW,
		canvasH:  canvasH,
		centerX:  float64(canvasW) / 2,
		centerY:  float64(canvasH) / 2,
		rx:       logoMarkRX * scale,
		ry:       logoMarkRY * scale,
		cornerH:  logoMarkCornerH * scale,
		tightGap: logoMarkTightGap * scale,
	}
}

// drawMark paints the two stacked hollow diamonds onto g (cleared first),
// using the given scaled dimensions and an animatable gap between the two
// diamond centers. Pass d.tightGap for the resting/brand state; larger
// values "open the jaws" during the chomp animation.
func drawMark(g *dotGrid, d markDims, gap float64) {
	for i := range g.dots {
		g.dots[i] = false
	}
	offs := [2]float64{-gap / 2, +gap / 2}
	for i := 1; i >= 0; i-- {
		cy := d.centerY + offs[i]
		g.fillBluntDiamondRing(
			d.centerX, cy,
			d.rx, d.ry,
			d.rx*logoMarkInnerRxRatio, d.ry*logoMarkInnerRyRatio,
			d.cornerH,
		)
	}
}

// rasterizeWordmarkLetters renders the brand wordmark PNG into a row of
// braille cells exactly dstCols wide AND returns the dst-cell column
// bounds [start, end) of every individual letter inside those cells.
//
// Letters are segmented in the source image by a vertical-projection
// histogram (a letter is a run of source columns whose column-summed
// ink count > 0, terminated by minGapWidth zero-ink columns). Each
// letter is then rasterized into its own braille glyph from its source
// sub-region, and the per-letter glyphs are concatenated with blank
// gap cells whose widths preserve the source's letter-to-letter
// spacing.
//
// Because each letter is sourced from a disjoint slice of the input
// image, adjacent letters can never bleed into each other in the dst
// grid — there is no detection step downstream and no possible split-
// in-the-middle. The animator just reads letterRanges to know where
// every glyph sits.
func rasterizeWordmarkLetters(dstCols int) (wmCells [][]logoCell, letterRanges [][2]int) {
	if dstCols <= 0 {
		return nil, nil
	}
	img, err := decodeWordmark()
	if err != nil || img == nil {
		return nil, nil
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW == 0 || srcH == 0 {
		return nil, nil
	}

	// Composite-over-white luminance grid.
	lum := make([]uint8, srcW*srcH)
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			r, gn, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			rr := float64(r+(0xffff-a)) / 0xffff
			gg := float64(gn+(0xffff-a)) / 0xffff
			bb := float64(bl+(0xffff-a)) / 0xffff
			l := 0.2126*rr + 0.7152*gg + 0.0722*bb
			lum[y*srcW+x] = uint8(l * 255)
		}
	}

	// Ink bbox + per-source-column ink count, both using the same
	// threshold the rasterizer uses below.
	const inkThr = 128
	counts := make([]int, srcW)
	xMin, xMax, yMin, yMax := srcW, -1, srcH, -1
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			if lum[y*srcW+x] < inkThr {
				counts[x]++
				if x < xMin {
					xMin = x
				}
				if x > xMax {
					xMax = x
				}
				if y < yMin {
					yMin = y
				}
				if y > yMax {
					yMax = y
				}
			}
		}
	}
	if xMax < 0 {
		return nil, nil
	}

	// Segment letters via column histogram with a min-gap rule. minGapWidth=3
	// is comfortable across the "extend" wordmark (gaps in this brand
	// asset are 4–16 source columns wide; letter-internal columns always
	// have ink from a connecting curve/crossbar).
	type letterRange struct{ X0, X1 int }
	var letters []letterRange
	const minGapWidth = 3
	{
		inLetter := false
		start := 0
		lastLit := 0
		zeroRun := 0
		for x := xMin; x <= xMax; x++ {
			if counts[x] > 0 {
				if !inLetter {
					start = x
					inLetter = true
				}
				lastLit = x
				zeroRun = 0
			} else if inLetter {
				zeroRun++
				if zeroRun >= minGapWidth {
					letters = append(letters, letterRange{start, lastLit + 1})
					inLetter = false
				}
			}
		}
		if inLetter {
			letters = append(letters, letterRange{start, lastLit + 1})
		}
	}
	if len(letters) == 0 {
		return nil, nil
	}

	// Build alternating sequence: letter, gap, letter, gap, ..., letter.
	type elem struct {
		isLetter bool
		srcX0    int // for letters
		srcW     int
	}
	var seq []elem
	for i, l := range letters {
		seq = append(seq, elem{isLetter: true, srcX0: l.X0, srcW: l.X1 - l.X0})
		if i < len(letters)-1 {
			gapW := letters[i+1].X0 - l.X1
			if gapW > 0 {
				seq = append(seq, elem{isLetter: false, srcW: gapW})
			}
		}
	}

	srcSpan := 0
	for _, e := range seq {
		srcSpan += e.srcW
	}

	// Allocate dst cell widths to each element by cumulative rounding
	// — sum is guaranteed == dstCols. The last element absorbs any
	// remainder.
	cellWs := make([]int, len(seq))
	{
		cumSrc := 0
		prevCell := 0
		for i, e := range seq {
			cumSrc += e.srcW
			var targetCell int
			if i == len(seq)-1 {
				targetCell = dstCols
			} else {
				targetCell = int(math.Round(float64(cumSrc) * float64(dstCols) / float64(srcSpan)))
			}
			if targetCell > dstCols {
				targetCell = dstCols
			}
			if targetCell < prevCell {
				targetCell = prevCell
			}
			cellWs[i] = targetCell - prevCell
			prevCell = targetCell
		}
	}

	// Shared dst height from the overall aspect.
	cropH := yMax - yMin + 1
	scale := float64(dstCols*2) / float64(srcSpan)
	dstH := int(math.Round(float64(cropH) * scale))
	if dstH < 4 {
		dstH = 4
	}
	cellH := (dstH + 3) / 4

	elemCells := make([][][]logoCell, len(seq))
	for i, e := range seq {
		cw := cellWs[i]
		if cw == 0 {
			continue
		}
		if !e.isLetter {
			blank := make([][]logoCell, cellH)
			for cy := range blank {
				row := make([]logoCell, cw)
				for cx := range row {
					row[cx] = logoCell{glyph: " "}
				}
				blank[cy] = row
			}
			elemCells[i] = blank
			continue
		}
		// Inline letter rasterization (the closure above had a typo
		// guard; this is the canonical version).
		dw := cw * 2
		grid := newDotGrid(dw, dstH)
		srcXMax := e.srcX0 + e.srcW
		for y := 0; y < dstH; y++ {
			sy0 := yMin + int(float64(y)*float64(cropH)/float64(dstH))
			sy1 := yMin + int(float64(y+1)*float64(cropH)/float64(dstH))
			if sy1 > yMax+1 {
				sy1 = yMax + 1
			}
			if sy1 <= sy0 {
				sy1 = sy0 + 1
			}
			for x := 0; x < dw; x++ {
				sx0 := e.srcX0 + int(float64(x)*float64(e.srcW)/float64(dw))
				sx1 := e.srcX0 + int(float64(x+1)*float64(e.srcW)/float64(dw))
				if sx1 > srcXMax {
					sx1 = srcXMax
				}
				if sx1 <= sx0 {
					sx1 = sx0 + 1
				}
				var sum, n int
				for sy := sy0; sy < sy1; sy++ {
					for sx := sx0; sx < sx1; sx++ {
						sum += int(lum[sy*srcW+sx])
						n++
					}
				}
				if n > 0 && (sum/n) < inkThr {
					grid.set(x, y)
				}
			}
		}
		elemCells[i] = dotGridToCells(grid)
	}

	// Concatenate all elements into one cell grid, padding any short
	// rows with blank cells so the grid is rectangular. Track each
	// letter's dst-cell column bounds.
	wmCells = make([][]logoCell, cellH)
	for cy := range wmCells {
		wmCells[cy] = make([]logoCell, 0, dstCols)
	}
	cellX := 0
	for i, e := range seq {
		cw := cellWs[i]
		if cw == 0 {
			continue
		}
		cells := elemCells[i]
		startX := cellX
		for cy := 0; cy < cellH; cy++ {
			if cells != nil && cy < len(cells) {
				wmCells[cy] = append(wmCells[cy], cells[cy]...)
				for k := len(cells[cy]); k < cw; k++ {
					wmCells[cy] = append(wmCells[cy], logoCell{glyph: " "})
				}
			} else {
				for k := 0; k < cw; k++ {
					wmCells[cy] = append(wmCells[cy], logoCell{glyph: " "})
				}
			}
		}
		cellX += cw
		if e.isLetter {
			letterRanges = append(letterRanges, [2]int{startX, cellX})
		}
	}
	wmCells = trimBlankRows(wmCells)
	return wmCells, letterRanges
}

// dotGridToCells packs a dot grid into a row-major slice of logoCells,
// converting each 2×4 dot region into a braille character.
func dotGridToCells(g *dotGrid) [][]logoCell {
	if g == nil {
		return nil
	}
	cellW := (g.w + 1) / 2
	cellH := (g.h + 3) / 4
	rows := make([][]logoCell, cellH)
	for cy := 0; cy < cellH; cy++ {
		row := make([]logoCell, cellW)
		for cx := 0; cx < cellW; cx++ {
			var bits int
			for ly := 0; ly < 4; ly++ {
				for lx := 0; lx < 2; lx++ {
					dx := cx*2 + lx
					dy := cy*4 + ly
					if dy < g.h && dx < g.w && g.dots[dy*g.w+dx] {
						if ly < 3 {
							bits |= 1 << (uint(ly) + 3*uint(lx))
						} else if lx == 0 {
							bits |= 0x40
						} else {
							bits |= 0x80
						}
					}
				}
			}
			if bits == 0 {
				row[cx] = logoCell{glyph: " "}
			} else {
				row[cx] = logoCell{glyph: string(rune(0x2800 + bits)), lit: true}
			}
		}
		rows[cy] = row
	}
	return rows
}

func trimBlankRows(g [][]logoCell) [][]logoCell {
	isBlank := func(row []logoCell) bool {
		for _, c := range row {
			if c.lit {
				return false
			}
		}
		return true
	}
	i, j := 0, len(g)
	for i < j && isBlank(g[i]) {
		i++
	}
	for j > i && isBlank(g[j-1]) {
		j--
	}
	return g[i:j]
}

// logoLockup is the fully laid-out lockup for a given target width: the
// composed cell grid AND the animator's region list with each region's
// source bounds already filled in (mark first, then one entry per
// wordmark letter). The caller fills in Pos/Target/Spring per region.
type logoLockup struct {
	grid    [][]logoCell
	regions []logoRegion
}

// buildLogoLockup renders the mark + wordmark lockup into one cell grid
// exactly `cols` wide and returns it alongside the per-region bounds.
// Letters are rasterized one at a time from disjoint source slices and
// concatenated, so the dst grid carries letter boundaries by
// construction — no post-hoc detection is required.
func buildLogoLockup(cols int) logoLockup {
	if cols < 12 {
		cols = 12
	}

	padding := logoLockupPadding
	if cols-padding < 12 {
		padding = 0
	}
	avail := cols - padding
	if avail < 6 {
		avail = cols
		padding = 0
	}

	// Proportional split per the brand ratio, with a small shrink applied
	// to the wordmark so the text reads a bit smaller than the mark.
	markCols := int(math.Round(float64(avail) * logoMarkWidthFrac))
	wmCols := int(math.Round(float64(avail-markCols) * logoWordmarkShrink))

	// Wordmark too narrow to be legible — give all the width to the mark
	// and hide the wordmark.
	if wmCols < logoMinWordmarkW {
		markCols = cols
		wmCols = 0
		padding = 0
	}

	// Render the mark at the chosen size.
	d := markDimsFor(markCols)
	markDots := newDotGrid(d.canvasW, d.canvasH)
	drawMark(markDots, d, d.tightGap)
	markCells := trimBlankRows(dotGridToCells(markDots))

	// Render the wordmark, if shown, as a row of per-letter glyphs.
	var (
		wmCells       [][]logoCell
		wmLetterCells [][2]int
	)
	if wmCols > 0 {
		wmCells, wmLetterCells = rasterizeWordmarkLetters(wmCols)
	}

	wmStartX := markCols + padding
	grid := composeLockup(markCells, wmCells, cols, wmStartX)

	// Region 0: the mark. Its width on the grid is the width of the
	// trimmed mark cell rows (composeLockup paints them at columns 0..N).
	var regions []logoRegion
	markEnd := 0
	for _, row := range markCells {
		if len(row) > markEnd {
			markEnd = len(row)
		}
	}
	if markEnd > 0 {
		regions = append(regions, logoRegion{StartCol: 0, EndCol: markEnd})
	}

	// Regions 1..N: one per wordmark letter, with bounds translated
	// from wm-local cell coords into lockup coords. Each is labeled with
	// its ASCII character from logoWordmark (by detection order) so
	// effects like the scan can show a plain-text version of the glyph.
	word := []rune(logoWordmark)
	for li, r := range wmLetterCells {
		reg := logoRegion{
			StartCol: wmStartX + r[0],
			EndCol:   wmStartX + r[1],
		}
		if li < len(word) {
			reg.Char = word[li]
		}
		regions = append(regions, reg)
	}

	// Record each region's ink-center column so the chomp can align a
	// letter's glyph (not its blank-padded bounds) with the mark.
	for i := range regions {
		regions[i].InkMid = gridInkCenterX(grid, regions[i].StartCol, regions[i].EndCol)
	}

	return logoLockup{grid: grid, regions: regions}
}

// buildLogoGrid is a back-compat wrapper that returns just the cell
// grid. Tests use this directly; the animator uses buildLogoLockup so
// it also gets the per-region bounds in the same pass.
func buildLogoGrid(cols int) [][]logoCell {
	return buildLogoLockup(cols).grid
}

// composeLockup places the mark in the left columns and the wordmark
// starting at wmStartX, vertically centering each, and pads every row
// out to `cols` so the returned grid is rectangular.
func composeLockup(mark, wm [][]logoCell, cols, wmStartX int) [][]logoCell {
	markRows := len(mark)
	wmRows := len(wm)
	totalRows := markRows
	if wmRows > totalRows {
		totalRows = wmRows
	}
	if totalRows == 0 {
		return nil
	}

	markYOff := (totalRows - markRows) / 2
	wmYOff := (totalRows - wmRows) / 2

	out := make([][]logoCell, totalRows)
	for r := 0; r < totalRows; r++ {
		row := make([]logoCell, cols)
		for i := range row {
			row[i] = logoCell{glyph: " "}
		}
		if r >= markYOff && r < markYOff+markRows {
			mr := mark[r-markYOff]
			for x := 0; x < len(mr) && x < cols; x++ {
				row[x] = mr[x]
			}
		}
		if wm != nil && r >= wmYOff && r < wmYOff+wmRows {
			wr := wm[r-wmYOff]
			for x := 0; x < len(wr) && wmStartX+x < cols; x++ {
				row[wmStartX+x] = wr[x]
			}
		}
		out[r] = row
	}
	return out
}

// Logo shimmer endpoints: light sky blue base, near-white highlight.
// Matches the chomp demo so the brand reads the same in both surfaces.
var (
	logoBase      = [3]int{0x6E, 0xBE, 0xFF}
	logoHighlight = [3]int{0xF5, 0xFA, 0xFF}
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
	grid [][]logoCell
	cols int
	// outW is the width of the rendered output in cells. When larger
	// than cols the `cols`-wide content is centered within it and the
	// extra space is "bleed" room so regions flying in from off-screen
	// (the intro) remain visible across the whole terminal instead of
	// being clipped at the logo's own bounding box. Defaults to cols.
	outW int
	// outH is the height of the rendered output in cell rows. The grid
	// content is vertically centered within it. The setup wizard sets
	// this to a fixed "logo viewport" height (tall enough for the open
	// chomp jaws) for every state so the logo string height is constant
	// — the box below it never shifts when the chomp starts. Defaults
	// to the grid height.
	outH    int
	styles  []lipgloss.Style
	colorOn bool
}

// logoRegion is a single fly-in chunk of the lockup — the mark, or one
// letter glyph — with both its source bounds inside the buildLogoGrid
// output and the current animated screen position of its left edge.
// `Pos` springs toward `Target`; the difference (Pos - StartCol) is the
// horizontal offset applied when rendering this region's cells.
type logoRegion struct {
	StartCol, EndCol int     // source bounds in the grid (inclusive start, exclusive end)
	InkMid           float64 // center column of this region's lit cells (grid coords)
	Char             rune    // the ASCII letter this region renders (0 for the mark)
	Target           float64 // desired screen column for the region's left edge
	Pos              float64 // current animated screen column (springs toward Target)
	Vel              float64
	Spring           harmonica.Spring
	Delay            int // ticks before this region starts moving
	DelayFrame       int
}

// gridInkCenterX returns the mid-column of the lit cells within columns
// [start, end) across all grid rows. Falls back to the geometric center
// of the range when there is no ink. Used to align regions by their
// actual glyph ink rather than their (possibly blank-padded) bounds.
func gridInkCenterX(grid [][]logoCell, start, end int) float64 {
	lo, hi := 1<<30, -1
	for _, row := range grid {
		for c := start; c < end && c < len(row); c++ {
			if row[c].lit {
				if c < lo {
					lo = c
				}
				if c > hi {
					hi = c
				}
			}
		}
	}
	if hi < 0 {
		return float64(start+end) / 2
	}
	return float64(lo+hi) / 2
}

// render draws the logo for the given animation frame. Each region is
// painted at its current animated position (`Pos`); cells outside the
// terminal column range are dropped. A bright highlight band sweeps
// across the lit cells to give the brand a subtle shimmer.
func (l logoAnim) render(frame int, regions []logoRegion) string {
	if len(l.grid) == 0 {
		return ""
	}
	cols := l.cols
	rows := len(l.grid)
	outW := l.outW
	if outW < cols {
		outW = cols
	}
	outH := l.outH
	if outH < rows {
		outH = rows
	}
	// Center the content within the (possibly larger) output canvas.
	pad := (outW - cols) / 2
	vpad := (outH - rows) / 2

	// Compose this frame by placing each region at its current screen
	// position. Cells outside the output bounds are dropped — but the
	// output is the full terminal width and a fixed-tall viewport, so
	// fly-in regions stay visible the whole way in.
	composed := make([][]logoCell, outH)
	for r := range composed {
		composed[r] = make([]logoCell, outW)
		for c := range composed[r] {
			composed[r][c] = logoCell{glyph: " "}
		}
	}
	for _, reg := range regions {
		offset := int(math.Round(reg.Pos)) - reg.StartCol + pad
		for r := 0; r < rows; r++ {
			dstR := r + vpad
			if dstR < 0 || dstR >= outH {
				continue
			}
			row := l.grid[r]
			for srcC := reg.StartCol; srcC < reg.EndCol && srcC < len(row); srcC++ {
				dstC := srcC + offset
				if dstC < 0 || dstC >= outW {
					continue
				}
				if row[srcC].lit {
					composed[dstR][dstC] = row[srcC]
				}
			}
		}
	}

	// Shimmer sweep, measured in content space (x-pad) so the sweep
	// cadence is unchanged by the wider canvas.
	n := len(l.styles)
	bandW := float64(cols) / 4.0
	if bandW < 4 {
		bandW = 4
	}
	span := float64(cols) + 2*bandW
	// 0.5 cols/frame at 60 Hz ≈ 30 cols/sec, matching the old 1.6×14Hz.
	center := math.Mod(float64(frame)*0.5, span) - bandW

	var b strings.Builder
	for _, row := range composed {
		for x, cell := range row {
			if !cell.lit {
				b.WriteString(cell.glyph)
				continue
			}
			if !l.colorOn || n == 0 {
				b.WriteString(cell.glyph)
				continue
			}
			d := math.Abs(float64(x-pad) - center)
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
