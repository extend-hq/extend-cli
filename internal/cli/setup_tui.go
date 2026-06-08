package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/extend-hq/extend-cli/internal/config"
)

// setupStep is the wizard's linear state machine.
type setupStep int

const (
	stepRegion setupStep = iota
	stepKey
	stepValidating
	stepSkill
	stepDone
)

// logoPhase tracks the logo's animation state: regions are flying in,
// settled and idling, or being chomped.
type logoPhase int

const (
	phaseIntro logoPhase = iota
	phaseIdle
	phaseChomping
	phaseScanning
)

// scanSub tracks where in the scan effect (triggered by '@') we are:
// the jaws open, the mark sweeps right over the still text "scanning"
// each letter into a flying ASCII glyph, then the mark returns home.
type scanSub int

const (
	scanSubOpening scanSub = iota // jaws spring apart, mark at home
	scanSubSweep                  // mark glides right, wiping letters
	scanSubReturn                 // mark glides back home, jaws close
)

// Scan tuning at the 60 Hz logo tick.
const (
	scanFOpen     = 18   // frames for the jaws to open
	scanSweepSpd  = 0.45 // cols/frame the mark glides right
	scanReturnSpd = 1.1  // cols/frame the mark glides back home
	scanTrailCols = 6    // cols past the last letter before turning back
)

// scanGlyph is a scanned-out ASCII letter flying away to the left. Its
// X/Y are in logo-content cells (mapped to the screen in View, like the
// chomp particles).
type scanGlyph struct {
	X, Y   float64
	VX, VY float64
	Ch     rune
	Color  string
	Life   float64
}

// chompSub tracks where in the chomp animation we are for the letter
// currently in the mark's jaws.
type chompSub int

const (
	chompSubOpening     chompSub = iota // jaws spring apart
	chompSubApproach                    // letter slides into the gap
	chompSubSlam                        // jaws cubic-easeIn slam shut
	chompSubImpact                      // shake + particles, letter is gone
	chompSubConsolidate                 // remaining letters shift left
)

// Frame durations for each chomp sub-phase at the 60 Hz logo tick rate.
// Match the cmd/anim-demo values so the rhythm feels the same here.
const (
	chompFOpen        = 22
	chompFApproach    = 20
	chompFSlam        = 10
	chompFImpact      = 14
	chompFConsolidate = 14
)

// chompGapMultiplier is how wide the jaws open (relative to the resting
// tight gap) at the peak of phaseSubOpening. Matches the ~6.3× ratio used
// in cmd/anim-demo (tightGap=6 → chompGap=38).
const chompGapMultiplier = 6.3

// chompParticleChars and chompParticleColors are the debris glyphs and
// truecolor escapes spawned at the moment the jaws slam shut. Mirrored
// from cmd/anim-demo.
var chompParticleChars = []rune{
	'✦', '✧', '★', '✺', '✷', '✸', '✹', '✱',
	'❋', '❅', '❆', '✤', '✩', '✪', '⋆', '⁂',
	'◆', '◇', '◊', '◉', '◍', '◎', '●', '○',
	'•', '·', '°', '∗', '※',
}

var chompParticleColors = []string{
	"\x1b[1;38;2;255;90;90m",
	"\x1b[1;38;2;255;160;60m",
	"\x1b[1;38;2;255;220;90m",
	"\x1b[1;38;2;120;255;130m",
	"\x1b[1;38;2;90;220;255m",
	"\x1b[1;38;2;140;160;255m",
	"\x1b[1;38;2;220;110;255m",
	"\x1b[1;38;2;255;160;220m",
	"\x1b[1;38;2;255;255;255m",
}

// chompParticle is one bit of debris flying out from the chomp point.
type chompParticle struct {
	X, Y   float64
	VX, VY float64
	Ch     rune
	Color  string
	Life   float64
}

// spawnChompParticles fires n bits of debris outward from (x, y), biased
// upward, with a small downward gravity applied each tick.
func spawnChompParticles(x, y float64, n int) []chompParticle {
	out := make([]chompParticle, 0, n)
	for i := 0; i < n; i++ {
		// Wide spread: π/2 ± ~3π/8 gives debris arcing from steeply
		// upward to nearly sideways. Mix some that go mostly down so
		// the cloud immediately starts reaching the bottom.
		angle := -math.Pi/2 + (rand.Float64()-0.5)*math.Pi*1.5
		speed := 0.7 + rand.Float64()*1.9
		out = append(out, chompParticle{
			X:     x,
			Y:     y,
			VX:    math.Cos(angle) * speed,
			VY:    math.Sin(angle) * speed * 0.65,
			Ch:    chompParticleChars[rand.Intn(len(chompParticleChars))],
			Color: chompParticleColors[rand.Intn(len(chompParticleColors))],
			// Long lifespan so debris has time to arc up, fall, and
			// reach the bottom of the terminal well past the box.
			Life: 120 + rand.Float64()*60,
		})
	}
	return out
}

func updateChompParticles(ps []chompParticle) []chompParticle {
	out := ps[:0]
	for _, p := range ps {
		p.X += p.VX
		p.Y += p.VY
		// Slightly stronger gravity + lighter horizontal drag so
		// particles spread sideways and then drop quickly.
		p.VY += 0.10
		p.VX *= 0.985
		// Cap downward speed so very-long-lived particles don't blur
		// into solid streaks at the bottom of the screen.
		if p.VY > 1.8 {
			p.VY = 1.8
		}
		p.Life--
		if p.Life > 0 {
			out = append(out, p)
		}
	}
	return out
}

// regionChoice pairs a region selector with the URLs a human needs: the
// API host (for confirmation) and the dashboard origin (where they create
// the key).
type regionChoice struct {
	id        string
	title     string
	api       string
	dashboard string
}

var setupRegionChoices = []regionChoice{
	{id: "us", title: "United States", api: "api.extend.ai", dashboard: "https://dashboard.extend.ai"},
	{id: "eu", title: "European Union", api: "api.eu1.extend.ai", dashboard: "https://dashboard.eu1.extend.ai"},
}

// setupValidator verifies a key against a region. Injected so tests can
// drive the model without real network calls.
type setupValidator func(ctx context.Context, region, key string) error

// errEmptyKey is shown inline when the user submits an empty key.
var errEmptyKey = errors.New("please paste an API key")

type setupResult struct {
	region  regionChoice
	apiKey  string
	path    string
	saveErr error

	// installSkill records the user's choice to write the agent SKILL.md.
	// skillPath/skillErr are filled in by runSetup after it performs the
	// install (the TUI model doesn't have the command tree to render it).
	installSkill bool
	skillPath    string
	skillErr     error
}

type logoTickMsg struct{}
type validatedMsg struct{ err error }

func logoTickCmd() tea.Cmd {
	return tea.Tick(16*time.Millisecond, func(time.Time) tea.Msg { return logoTickMsg{} })
}

// UI styles. lipgloss degrades these to the terminal's color profile
// (and to no-op on NO_COLOR), so they're safe to define unconditionally.
var (
	stTagline  = lipgloss.NewStyle().Faint(true)
	stDivider  = lipgloss.NewStyle().Foreground(lipgloss.Color("#1E3A5F"))
	stHeading  = lipgloss.NewStyle().Bold(true)
	stSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("#93C5FD")).Bold(true)
	stDim      = lipgloss.NewStyle().Faint(true)
	stLink     = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6")).Bold(true).Underline(true)
	stGood     = lipgloss.NewStyle().Foreground(lipgloss.Color("#22C55E")).Bold(true)
	stBad      = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
	stBox      = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3B82F6")).
			Padding(1, 3)
)

type setupModel struct {
	ctx      context.Context
	colorOn  bool
	validate setupValidator

	step   setupStep
	cursor int
	region regionChoice
	apiKey string

	input  textinput.Model
	spin   spinner.Model
	valErr error

	// Logo animation state.
	styles  []lipgloss.Style
	width   int
	height  int
	cols    int
	lockup  logoLockup // grid + per-region bounds, rebuilt on resize
	grid    [][]logoCell
	frame   int
	regions []logoRegion // mark + each letter, with per-region spring state
	phase   logoPhase    // intro / idle / chomping
	// chompEatenIdx is the index into m.regions of the letter currently
	// being chomped. The mark is always at index 0, so letters are
	// indices 1..len(regions)-1 and we chomp them in order.
	chompEatenIdx int

	// Active during phaseChomping. The mark is re-rendered parametrically
	// each frame using chompMarkDims + chompGap so the jaws can animate;
	// particles + camera shake provide the impact effects.
	chompSub         chompSub
	chompPF          int      // frames elapsed in the current sub-phase
	chompMarkDims    markDims // scaled mark dimensions matching current cols
	chompMarkScratch *dotGrid // reusable per-frame braille buffer
	chompGap         float64
	chompGapVel      float64
	chompGapSpring   harmonica.Spring
	chompGapWide     float64 // peak jaw separation
	chompSlamStart   float64 // gap at the moment slam phase begins
	chompParticles   []chompParticle
	chompShakeX      int
	chompShakeY      int
	chompShakeI      float64

	// Active during phaseScanning (triggered by '@'). The mark glides
	// right over the still text, wiping each letter and emitting a flying
	// ASCII glyph. Reuses chompMarkDims/Scratch/Gap for the open jaws.
	scanSub       scanSub
	scanPF        int
	scanMarkX     float64 // mark ink-center column, content coords
	scanLetterIdx int     // next letter region to scan out
	scanGlyphs    []scanGlyph

	result   *setupResult
	canceled bool
	quitting bool
}

func newSetupModel(ctx context.Context, colorOn bool, preRegion string, validate setupValidator) setupModel {
	ti := textinput.New()
	ti.Placeholder = "sk_..."
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Prompt = "› "
	ti.CharLimit = 256
	ti.SetWidth(46)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	if colorOn {
		sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6"))
	}

	cursor := 0
	for i, r := range setupRegionChoices {
		if r.id == preRegion {
			cursor = i
		}
	}

	m := setupModel{
		ctx:      ctx,
		colorOn:  colorOn,
		validate: validate,
		step:     stepRegion,
		cursor:   cursor,
		input:    ti,
		spin:     sp,
	}
	if colorOn {
		m.styles = buildLogoStyles()
	}
	// Don't build the logo grid yet — the first tea.WindowSizeMsg will
	// trigger setWidth() with the real terminal width, and that's when
	// the intro fly-in kicks in. Building it here with a placeholder
	// 96-col grid would either play the intro at the wrong size or
	// force a re-animation when the real size arrives.
	return m
}

// flyInSpring is shared by all logo regions during the intro fly-in.
// Snappy angular freq with a touch of underdamping so each region lands
// with a small overshoot/settle rather than a flat ease. Tuned for the
// 60 Hz logo tick.
func flyInSpring() harmonica.Spring {
	return harmonica.NewSpring(harmonica.FPS(60), 8.0, 0.65)
}

// cloneLockupRegions returns a fresh copy of the lockup's pre-filled
// region bounds (StartCol/EndCol only) ready for the caller to fill in
// per-region spring state.
func (m *setupModel) cloneLockupRegions() []logoRegion {
	out := make([]logoRegion, len(m.lockup.regions))
	copy(out, m.lockup.regions)
	return out
}

// flyInStartX is the content-space X just past the right edge of the
// SCREEN (not the logo's own bounding box). The logo renders into an
// m.width-wide canvas with the content centered, so a content-space X of
// (m.width - pad) maps to the right screen edge; +4 nudges it fully
// off-screen before it springs home.
func (m *setupModel) flyInStartX() float64 {
	startX := float64(m.cols) + 4
	if m.width > m.cols {
		pad := (m.width - m.cols) / 2
		startX = float64(m.width-pad) + 4
	}
	return startX
}

// initFlyIn reinitializes m.regions to "off-screen-right, springing
// home" state, with each region staggered slightly after the previous
// so the mark + letters land one at a time, left to right. Used for the
// very first intro.
func (m *setupModel) initFlyIn() {
	m.regions = m.cloneLockupRegions()
	startX := m.flyInStartX()
	for i := range m.regions {
		r := &m.regions[i]
		r.Target = float64(r.StartCol)
		r.Pos = startX
		r.Vel = -2.0 // initial leftward shove for momentum
		r.Spring = flyInSpring()
		r.Delay = i * 5 // ~83 ms between regions at 60 Hz
		r.DelayFrame = 0
	}
	m.phase = phaseIntro
	m.chompEatenIdx = 0
}

// flyInLetters rebuilds the regions and flies only the wordmark letters
// back in from off-screen right, holding the mark (region 0) settled in
// place. Used after a chomp eats the whole word: the diamond stays put
// and just the text re-assembles beside it.
func (m *setupModel) flyInLetters() {
	m.regions = m.cloneLockupRegions()
	startX := m.flyInStartX()
	for i := range m.regions {
		r := &m.regions[i]
		r.Target = float64(r.StartCol)
		r.Spring = flyInSpring()
		r.DelayFrame = 0
		if i == 0 {
			// Mark holds its settled position.
			r.Pos = r.Target
			r.Vel = 0
			r.Delay = 0
			continue
		}
		r.Pos = startX
		r.Vel = -2.0
		// Stagger letters; the first letter starts immediately (the mark
		// isn't animating, so don't reserve a slot for it).
		r.Delay = (i - 1) * 5
	}
	m.phase = phaseIntro
	m.chompEatenIdx = 0
}

// snapRegionsToIdle rebuilds regions at the current grid size and parks
// each one at its natural position with zero velocity. Used on terminal
// resizes so the lockup just resizes in place instead of replaying the
// intro animation.
func (m *setupModel) snapRegionsToIdle() {
	m.regions = m.cloneLockupRegions()
	spring := flyInSpring()
	for i := range m.regions {
		r := &m.regions[i]
		r.Target = float64(r.StartCol)
		r.Pos = float64(r.StartCol)
		r.Vel = 0
		r.Spring = spring
		r.Delay = 0
		r.DelayFrame = 0
	}
	m.phase = phaseIdle
	m.chompEatenIdx = 0
}

// setWidth recomputes the logo column count (and re-rasterizes the
// embedded PNG to fit) whenever the terminal size changes. The logo
// claims the box's inner width: terminal width minus the rounded border
// (2) and horizontal padding (6).
func (m *setupModel) setWidth(w int) {
	if w <= 0 {
		w = 96
	}
	m.width = w
	cols := w - 8
	if cols > 120 {
		cols = 120
	}
	if cols < 44 {
		cols = 44
	}
	if cols == m.cols && m.grid != nil {
		return
	}
	firstInit := m.grid == nil
	m.cols = cols
	m.lockup = buildLogoLockup(cols)
	m.grid = m.lockup.grid
	if firstInit {
		// First valid size — play the intro fly-in.
		m.initFlyIn()
	} else {
		// Resize after the intro has already kicked off — rebuild
		// regions at the new size but snap them to settled positions
		// so we don't replay the animation every time the terminal
		// changes dimensions.
		m.snapRegionsToIdle()
	}
	if iw := cols - 6; iw > 12 {
		if iw > 60 {
			iw = 60
		}
		m.input.SetWidth(iw)
	}
}

// markCenterCol is the ink-center column of the mark (the "throat" the
// chomped letter slides into). Returns 0 if the mark region hasn't been
// determined yet. Uses the mark's lit-cell center rather than its region
// bounds so letters align with the visible diamond.
func (m setupModel) markCenterCol() float64 {
	if len(m.regions) == 0 {
		return 0
	}
	return m.regions[0].InkMid
}

// logoBusy reports whether a logo effect (chomp or scan) is mid-flight,
// so triggers don't stack.
func (m setupModel) logoBusy() bool {
	return m.phase == phaseChomping || m.phase == phaseScanning
}

// startChomp transitions the logo into the chomp animation: the leftmost
// letter (regions[1]) is given a target inside the mark, springs of all
// regions are made a bit snappier, and the tick handler will eat letters
// one at a time, shifting the rest left to consolidate. When the last
// letter is consumed, the intro fly-in is replayed.
// chompMarkCols mirrors buildLogoGrid's per-resize mark sizing so the
// parametric mark we render during chomp matches the size of the mark
// baked into the static grid.
func (m setupModel) chompMarkCols() int {
	avail := m.cols - logoLockupPadding
	if avail < 6 {
		avail = m.cols
	}
	markCols := int(math.Round(float64(avail) * logoMarkWidthFrac))
	if markCols < 8 {
		markCols = 8
	}
	if markCols > m.cols {
		markCols = m.cols
	}
	return markCols
}

// chompLayout returns the geometry of the chomp compose canvas: its
// height (composedH — grows to fit the open jaws), the vertical offset
// that centers the grid-content letters within it (vpad), the vertical
// offset of the parametric mark (markYOff), and the mark's cell height
// (markH). The canvas is sized to whichever is taller (resting lockup or
// open mark) and both are vertically CENTERED, so the mark's center row
// stays put and the jaws expand symmetrically. The logo is composited as
// a transparent overlay (see composeScreen), so this taller-when-open
// canvas overlaps the box in front of it rather than pushing it down.
// Shared by renderChomp (to draw) and View (to position particles).
func (m setupModel) chompLayout() (composedH, vpad, markYOff, markH int) {
	gridH := len(m.grid)
	if m.chompMarkDims.canvasH > 0 {
		markH = m.chompMarkDims.canvasH / 4 // dotGridToCells height
	}
	composedH = gridH
	if markH > composedH {
		composedH = markH
	}
	vpad = (composedH - gridH) / 2
	markYOff = (composedH - markH) / 2
	return composedH, vpad, markYOff, markH
}

// startChomp transitions into the full chomp animation: the jaws are
// re-rendered parametrically each frame so they can spring open and
// slam shut, letters slide into the throat, particles fire on impact,
// and the camera shakes. When the last letter is consumed the intro
// fly-in replays and we settle back to idle.
// setupParametricMark (re)creates the scratch buffer + jaw-gap springs
// for the parametric mark shared by the chomp and scan effects.
func (m *setupModel) setupParametricMark() {
	m.chompMarkDims = markDimsFor(m.chompMarkCols())
	m.chompMarkScratch = newDotGrid(m.chompMarkDims.canvasW, m.chompMarkDims.canvasH)
	m.chompGap = m.chompMarkDims.tightGap
	m.chompGapVel = 0
	m.chompGapSpring = harmonica.NewSpring(harmonica.FPS(60), 8.0, 0.55)
	m.chompGapWide = m.chompMarkDims.tightGap * chompGapMultiplier
}

func (m *setupModel) startChomp() {
	if len(m.regions) < 2 || m.grid == nil {
		return
	}
	m.setupParametricMark()
	m.chompSlamStart = 0
	m.chompParticles = nil
	m.chompShakeX = 0
	m.chompShakeY = 0
	m.chompShakeI = 0

	letterSpring := harmonica.NewSpring(harmonica.FPS(60), 9.0, 0.6)
	for i := range m.regions {
		r := &m.regions[i]
		r.Spring = letterSpring
		r.Delay = 0
		r.DelayFrame = 0
	}
	m.chompSub = chompSubOpening
	m.chompPF = 0
	m.phase = phaseChomping
}

// startScan transitions into the scan effect: the jaws open and the mark
// glides right over the still text, wiping each letter into a flying
// ASCII glyph, then returns home and the text re-assembles.
func (m *setupModel) startScan() {
	if len(m.regions) < 2 || m.grid == nil {
		return
	}
	m.setupParametricMark()
	m.scanMarkX = m.regions[0].InkMid // mark starts at its home center
	m.scanLetterIdx = 1
	m.scanGlyphs = nil
	// Letters hold still at their settled positions throughout the scan.
	for i := range m.regions {
		r := &m.regions[i]
		r.Pos = float64(r.StartCol)
		r.Target = float64(r.StartCol)
		r.Vel = 0
	}
	m.scanSub = scanSubOpening
	m.scanPF = 0
	m.phase = phaseScanning
}

// advanceLogo updates every region's spring each tick. In the intro
// phase it watches for settle and transitions to idle. In the chomp
// phase it steps through the sub-phases (Open → Approach → Slam →
// Impact → Consolidate), drives the jaw gap + particle physics, and
// loops back to fly-in once everything has been eaten.
func (m *setupModel) advanceLogo() {
	if m.grid == nil {
		return
	}
	// Region springs always tick — even in chomp, letters animating
	// toward their (possibly-just-shifted) targets need to be updated.
	for i := range m.regions {
		r := &m.regions[i]
		if r.DelayFrame < r.Delay {
			r.DelayFrame++
			continue
		}
		r.Pos, r.Vel = r.Spring.Update(r.Pos, r.Vel, r.Target)
	}
	m.chompParticles = updateChompParticles(m.chompParticles)

	switch m.phase {
	case phaseIntro:
		settled := true
		for _, r := range m.regions {
			if math.Abs(r.Pos-r.Target) > 0.4 {
				settled = false
				break
			}
		}
		if settled {
			m.phase = phaseIdle
		}

	case phaseChomping:
		if len(m.regions) < 2 {
			// Word fully eaten — hold the mark in place and fly just the
			// wordmark text back in.
			m.chompParticles = nil
			m.chompShakeX, m.chompShakeY, m.chompShakeI = 0, 0, 0
			m.flyInLetters()
			return
		}

		m.chompPF++
		switch m.chompSub {
		case chompSubOpening:
			// Jaws spring open. Letter holds position.
			m.chompGap, m.chompGapVel = m.chompGapSpring.Update(m.chompGap, m.chompGapVel, m.chompGapWide)
			if m.chompPF >= chompFOpen {
				m.chompSub = chompSubApproach
				m.chompPF = 0
				// Aim the letter so its INK center lands on the mark's
				// ink center. Aligning ink-to-ink (not region bounds)
				// avoids the half-cell left bias from blank-padded glyph
				// regions; rounding keeps it on a whole cell.
				next := &m.regions[1]
				next.Target = float64(next.StartCol) + math.Round(m.markCenterCol()-next.InkMid)
			}
		case chompSubApproach:
			// Hold jaws open while the letter slides in.
			m.chompGap, m.chompGapVel = m.chompGapSpring.Update(m.chompGap, m.chompGapVel, m.chompGapWide)
			if m.chompPF >= chompFApproach {
				m.chompSub = chompSubSlam
				m.chompPF = 0
				m.chompSlamStart = m.chompGap
			}
		case chompSubSlam:
			// Aggressive cubic-easeIn slam — no spring, just rush.
			t := float64(m.chompPF) / float64(chompFSlam)
			t = t * t * t
			m.chompGap = m.chompSlamStart + (m.chompMarkDims.tightGap-m.chompSlamStart)*t
			m.chompGapVel = 0
			if m.chompPF >= chompFSlam {
				// CHOMP — debris + shake.
				cx := m.markCenterCol() + 0.5
				cy := float64(len(m.grid)) / 2
				m.chompParticles = append(m.chompParticles, spawnChompParticles(cx, cy, 22)...)
				m.chompShakeI = 2.6
				m.chompSub = chompSubImpact
				m.chompPF = 0
			}
		case chompSubImpact:
			// Jaws locked, shake decays.
			m.chompGap = m.chompMarkDims.tightGap
			m.chompGapVel = 0
			m.chompShakeX = int(math.Round((rand.Float64()*2 - 1) * m.chompShakeI))
			m.chompShakeY = int(math.Round((rand.Float64()*2 - 1) * m.chompShakeI * 0.5))
			m.chompShakeI *= 0.78
			if m.chompShakeI < 0.25 {
				m.chompShakeI = 0
				m.chompShakeX = 0
				m.chompShakeY = 0
			}
			if m.chompPF >= chompFImpact {
				// Splice the eaten letter out, shift the rest left.
				eaten := m.regions[1]
				var shift float64
				if len(m.regions) >= 3 {
					shift = float64(m.regions[2].StartCol - eaten.StartCol)
				}
				m.regions = append(m.regions[:1], m.regions[2:]...)
				for i := 1; i < len(m.regions); i++ {
					m.regions[i].Target -= shift
				}
				m.chompSub = chompSubConsolidate
				m.chompPF = 0
			}
		case chompSubConsolidate:
			// Gap rests tight; remaining letters slide left.
			m.chompGap, m.chompGapVel = m.chompGapSpring.Update(m.chompGap, m.chompGapVel, m.chompMarkDims.tightGap)
			if m.chompPF >= chompFConsolidate {
				if len(m.regions) >= 2 {
					m.chompSub = chompSubOpening
					m.chompPF = 0
				} else {
					m.chompParticles = nil
					m.chompShakeX, m.chompShakeY, m.chompShakeI = 0, 0, 0
					m.initFlyIn()
				}
			}
		}

	case phaseScanning:
		m.scanGlyphs = updateScanGlyphs(m.scanGlyphs)
		m.scanPF++
		switch m.scanSub {
		case scanSubOpening:
			m.chompGap, m.chompGapVel = m.chompGapSpring.Update(m.chompGap, m.chompGapVel, m.chompGapWide)
			if m.scanPF >= scanFOpen {
				m.scanSub = scanSubSweep
				m.scanPF = 0
			}
		case scanSubSweep:
			// Jaws held open; the mark glides right over the still text.
			m.chompGap = m.chompGapWide
			m.chompGapVel = 0
			m.scanMarkX += scanSweepSpd
			// Scan out each letter as the mark's center crosses its ink
			// center: hide it (renderScan wipes left-of-mark cells) and
			// emit a flying ASCII glyph in its place.
			for m.scanLetterIdx < len(m.regions) &&
				m.scanMarkX >= m.regions[m.scanLetterIdx].InkMid {
				r := m.regions[m.scanLetterIdx]
				m.scanGlyphs = append(m.scanGlyphs, spawnScanGlyph(r, float64(len(m.grid))/2))
				m.scanLetterIdx++
			}
			// Past the last letter (+trail) — turn back.
			last := m.regions[len(m.regions)-1]
			if m.scanMarkX >= float64(last.EndCol)+scanTrailCols {
				m.scanSub = scanSubReturn
				m.scanPF = 0
			}
		case scanSubReturn:
			// Jaws close back to tight while the mark glides home.
			m.chompGap, m.chompGapVel = m.chompGapSpring.Update(m.chompGap, m.chompGapVel, m.chompMarkDims.tightGap)
			home := m.regions[0].InkMid
			m.scanMarkX -= scanReturnSpd
			if m.scanMarkX <= home {
				m.scanMarkX = home
				// Home again — replay the wordmark fly-in beside the mark.
				m.scanGlyphs = nil
				m.flyInLetters()
			}
		}
	}
}

// spawnScanGlyph creates a flying ASCII glyph for a just-scanned letter,
// launched from the letter's position and drifting left (and slightly
// up) as it fades.
func spawnScanGlyph(r logoRegion, y float64) scanGlyph {
	ch := r.Char
	if ch == 0 {
		ch = '*'
	}
	return scanGlyph{
		X:     r.InkMid,
		Y:     y,
		VX:    -0.9 - rand.Float64()*0.4,
		VY:    -0.08 + rand.Float64()*0.16,
		Ch:    ch,
		Color: "\x1b[1;38;2;124;252;180m", // scan green
		Life:  90 + rand.Float64()*30,
	}
}

func updateScanGlyphs(gs []scanGlyph) []scanGlyph {
	out := gs[:0]
	for _, g := range gs {
		g.X += g.VX
		g.Y += g.VY
		g.VX *= 0.99
		g.Life--
		if g.Life > 0 {
			out = append(out, g)
		}
	}
	return out
}

func (m setupModel) Init() tea.Cmd {
	return tea.Batch(logoTickCmd(), textinput.Blink)
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.setWidth(msg.Width)
		return m, nil

	case logoTickMsg:
		m.frame++
		if m.frame > 1<<20 {
			m.frame = 0
		}
		m.advanceLogo()
		return m, logoTickCmd()

	case spinner.TickMsg:
		if m.step != stepValidating {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case validatedMsg:
		if msg.err != nil {
			m.valErr = msg.err
			m.step = stepKey
			return m, m.input.Focus()
		}
		path, err := config.Save(config.File{
			Region: m.region.id,
			Auth:   &config.Auth{Type: config.AuthAPIKey, APIKey: m.apiKey},
		})
		m.result = &setupResult{region: m.region, apiKey: m.apiKey, path: path, saveErr: err}
		if err != nil {
			// Couldn't save — finish so runSetup reports the error.
			m.step = stepDone
			m.quitting = true
			return m, tea.Quit
		}
		// Offer to install the agent skill before finishing.
		m.step = stepSkill
		m.cursor = 0 // default to "Yes"
		return m, nil

	case tea.MouseClickMsg:
		// Click anywhere on the top rows (the logo area) triggers the
		// chomp easter egg, as long as no logo effect is already running.
		// MouseClickMsg is press-only — release fires MouseReleaseMsg.
		if !m.logoBusy() && msg.Y < len(m.grid)+2 {
			m.startChomp()
			return m, nil
		}

	case tea.KeyPressMsg:
		// `!` chomps and `@` scans, from anywhere except the API-key text
		// input (where they're legitimate characters).
		if m.step != stepKey && !m.logoBusy() {
			switch msg.String() {
			case "!":
				m.startChomp()
				return m, nil
			case "@":
				m.startScan()
				return m, nil
			}
		}
		return m.handleKey(msg)
	}

	if m.step == stepKey {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m setupModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.canceled = true
		m.quitting = true
		return m, tea.Quit
	}

	switch m.step {
	case stepRegion:
		switch msg.String() {
		case "esc", "q":
			m.canceled = true
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(setupRegionChoices)-1 {
				m.cursor++
			}
		case "enter", "right", "l", " ":
			m.region = setupRegionChoices[m.cursor]
			m.step = stepKey
			m.valErr = nil
			return m, m.input.Focus()
		}
		return m, nil

	case stepKey:
		switch msg.String() {
		case "esc":
			m.step = stepRegion
			m.valErr = nil
			m.input.Blur()
			return m, nil
		case "enter":
			key := strings.TrimSpace(m.input.Value())
			if key == "" {
				m.valErr = errEmptyKey
				return m, nil
			}
			m.apiKey = key
			m.valErr = nil
			m.step = stepValidating
			m.input.Blur()
			return m, tea.Batch(m.spin.Tick, m.validateCmd())
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

	case stepValidating:
		return m, nil

	case stepSkill:
		switch msg.String() {
		case "up", "k", "down", "j", "left", "right", "h", "l", "tab":
			m.cursor = 1 - m.cursor // toggle Yes/No
			return m, nil
		case "y", "Y":
			m.cursor = 0
		case "n", "N":
			m.cursor = 1
		case "enter", " ":
			// fall through to commit below
		case "esc", "q":
			m.cursor = 1 // treat as "No"
		default:
			return m, nil
		}
		if m.result != nil {
			m.result.installSkill = m.cursor == 0
		}
		m.step = stepDone
		m.quitting = true
		return m, tea.Quit

	case stepDone:
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m setupModel) validateCmd() tea.Cmd {
	ctx := m.ctx
	region := m.region.id
	key := m.apiKey
	validate := m.validate
	return func() tea.Msg {
		return validatedMsg{err: validate(ctx, region, key)}
	}
}

// screenLayer is one element to composite onto the screen buffer.
// When transparent is true, blank (space) cells in s do NOT overwrite
// what's beneath them — this is what lets the logo's diamonds overlap
// the box in front while the box still shows through the gaps. (Lipgloss
// v2 layers are opaque rectangles, so they can't do this.)
type screenLayer struct {
	s           string
	x, y        int
	transparent bool
}

// composeScreen paints the layers, in order, onto a width×height cell
// buffer and renders it to a single styled string exactly that size.
// Later layers draw on top. Cells outside the buffer are clipped. Each
// layer is first drawn into its own temp buffer (StyledString.Draw is
// area-bounded and ANSI-correct), then its cells are copied to the
// screen; for transparent layers, blank cells are skipped so what's
// beneath shows through.
func composeScreen(width, height int, layers ...screenLayer) string {
	buf := lipgloss.NewCanvas(width, height)
	for _, l := range layers {
		lw, lh := lipgloss.Size(l.s)
		if lw <= 0 || lh <= 0 {
			continue
		}
		tmp := lipgloss.NewCanvas(lw, lh)
		uv.NewStyledString(l.s).Draw(tmp, tmp.Bounds())
		for y := 0; y < lh; y++ {
			sy := l.y + y
			if sy < 0 || sy >= height {
				continue
			}
			for x := 0; x < lw; x++ {
				sx := l.x + x
				if sx < 0 || sx >= width {
					continue
				}
				c := tmp.CellAt(x, y)
				if c == nil {
					continue
				}
				if l.transparent && strings.TrimSpace(c.Content) == "" {
					continue
				}
				cc := *c
				buf.SetCell(sx, sy, &cc)
			}
		}
	}
	return buf.Render()
}

func (m setupModel) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.quitting {
		return v
	}

	// Build the box (tagline + body + footer). The logo is NOT stacked
	// into it — it's composited on top as a transparent overlay so the
	// chomp diamonds can grow in front of the box without moving it.
	logo := m.renderLogo()
	tagline := stTagline.Render("Document Processing APIs")
	divider := stDivider.Render(strings.Repeat("─", m.cols))
	content := m.renderStep()
	content = lipgloss.NewStyle().Width(m.cols).Height(8).Render(content)
	footer := stDim.Render(m.footerHint())
	inner := lipgloss.JoinVertical(
		lipgloss.Center,
		tagline,
		divider,
		"",
		content,
		"",
		footer,
	)
	boxed := stBox.Render(inner)

	// Without a terminal size yet, return a simple stacked fallback.
	if m.width <= 0 || m.height <= 0 {
		v.Content = lipgloss.JoinVertical(lipgloss.Center, logo, "", boxed)
		return v
	}

	// Layout rule: the box is ALWAYS fully visible at a stable position;
	// the logo is anchored just above it and never pushes or overlaps the
	// box from layout. Only the chomp animation deliberately grows the
	// logo downward in front of the box. The box position depends solely
	// on the resting logo height (len(m.grid)), so the animated logo
	// height never moves it.
	gridH := len(m.grid)
	boxW, boxH := lipgloss.Size(boxed)
	const gap = 1
	restingH := gridH + gap + boxH

	boxX := (m.width - boxW) / 2
	if boxX < 0 {
		boxX = 0
	}

	// When the resting stack fits, center it; otherwise anchor the box so
	// it stays fully visible and let the logo overflow the top of the
	// screen (clipped) rather than overlapping the box.
	var boxY int
	if restingH <= m.height {
		boxY = (m.height-restingH)/2 + gridH + gap
	} else {
		boxY = m.height - boxH
	}
	if boxY+boxH > m.height {
		boxY = m.height - boxH
	}
	if boxY < 0 {
		boxY = 0
	}

	// The resting logo content sits in the gridH rows just above the box.
	// The logo string renders full-width (so fly-in/jaws aren't clipped)
	// with its content vertically centered, so offset by half the extra
	// height keeps the resting content fixed while the chomp jaws expand
	// symmetrically around it (and overflow the top if needed).
	restLogoTop := boxY - gap - gridH
	logoH := lipgloss.Height(logo)
	logoY := restLogoTop - (logoH-gridH)/2

	layers := []screenLayer{
		{s: boxed, x: boxX, y: boxY, transparent: false},
		{s: logo, x: 0, y: logoY, transparent: true},
	}

	// Chomp particles on top of everything, in screen coords. They're
	// positioned relative to the logo content's top-left: horizontally
	// centered (contentX) and vertically at the chomp canvas's vpad.
	if m.phase == phaseChomping && len(m.chompParticles) > 0 {
		_, vpad, _, _ := m.chompLayout()
		contentX := (m.width - m.cols) / 2
		contentTopY := logoY + vpad
		for _, p := range m.chompParticles {
			px := contentX + int(math.Round(p.X)) + m.chompShakeX
			py := contentTopY + int(math.Round(p.Y)) + m.chompShakeY
			if px < 0 || px >= m.width || py < 0 || py >= m.height {
				continue
			}
			layers = append(layers, screenLayer{
				s:           p.Color + string(p.Ch) + "\x1b[0m",
				x:           px,
				y:           py,
				transparent: true,
			})
		}
	}

	// Scanned-out ASCII letters flying away to the left.
	if m.phase == phaseScanning && len(m.scanGlyphs) > 0 {
		_, vpad, _, _ := m.chompLayout()
		contentX := (m.width - m.cols) / 2
		contentTopY := logoY + vpad
		for _, g := range m.scanGlyphs {
			px := contentX + int(math.Round(g.X))
			py := contentTopY + int(math.Round(g.Y))
			if px < 0 || px >= m.width || py < 0 || py >= m.height {
				continue
			}
			layers = append(layers, screenLayer{
				s:           g.Color + string(g.Ch) + "\x1b[0m",
				x:           px,
				y:           py,
				transparent: true,
			})
		}
	}

	v.Content = composeScreen(m.width, m.height, layers...)
	return v
}

func (m setupModel) renderLogo() string {
	if len(m.grid) == 0 {
		return stHeading.Render("E X T E N D")
	}
	if m.phase == phaseChomping && m.chompMarkScratch != nil {
		if s := m.renderChomp(); s != "" {
			return s
		}
	}
	if m.phase == phaseScanning && m.chompMarkScratch != nil {
		if s := m.renderScan(); s != "" {
			return s
		}
	}
	anim := logoAnim{
		grid:    m.grid,
		cols:    m.cols,
		outW:    m.width,
		styles:  m.styles,
		colorOn: m.colorOn,
	}
	if s := anim.render(m.frame, m.regions); s != "" {
		return s
	}
	return stHeading.Render("E X T E N D")
}

// renderChomp composites a frame of the chomp animation: the mark is
// re-rendered parametrically each frame so the jaws actually open and
// slam shut, the wordmark letters animate via their springs, particles
// are overlaid, and the whole image is offset by a few cells for the
// camera shake on impact.
func (m setupModel) renderChomp() string {
	cols := m.cols
	gridH := len(m.grid)
	if gridH == 0 || cols == 0 {
		return ""
	}

	// Output canvas is the full terminal width with the cols-wide
	// content centered (pad), so jaws and sliding letters that exceed
	// the logo's own width aren't clipped at its bounding box. The
	// camera shake is applied as a content offset WITHIN this fixed
	// canvas (not by prepending whitespace) so the logo string stays
	// exactly outW×composedH and never overflows the screen.
	outW := m.width
	if outW < cols {
		outW = cols
	}
	pad := (outW-cols)/2 + m.chompShakeX

	// 1) Render the parametric mark with the current jaw gap.
	drawMark(m.chompMarkScratch, m.chompMarkDims, m.chompGap)
	markCells := dotGridToCells(m.chompMarkScratch)

	composedH, vpad, markYOff, _ := m.chompLayout()
	vpad += m.chompShakeY
	markYOff += m.chompShakeY

	// 2) Per-row "behind the mark" silhouette mask: for each mark row,
	//    the run of columns from the leftmost to the rightmost lit cell
	//    is occluded. Letter cells inside this range are dropped so the
	//    letter visibly disappears INTO the diamond as the jaws close
	//    on it — not just behind the thin outline strokes. Indexed by
	//    composed-grid row.
	maskLo := make([]int, composedH)
	maskHi := make([]int, composedH)
	for r := range maskLo {
		maskLo[r] = -1
		maskHi[r] = -1
	}
	for r, row := range markCells {
		dstR := r + markYOff
		if dstR < 0 || dstR >= composedH {
			continue
		}
		lo, hi := -1, -1
		for c, cell := range row {
			if cell.lit {
				if lo < 0 {
					lo = c
				}
				hi = c
			}
		}
		maskLo[dstR] = lo
		maskHi[dstR] = hi
	}

	// Compose buffer (composedH × outW) of logoCells.
	composed := make([][]logoCell, composedH)
	for r := range composed {
		composed[r] = make([]logoCell, outW)
		for c := range composed[r] {
			composed[r][c] = logoCell{glyph: " "}
		}
	}

	// 3) Draw letters first — anywhere a letter cell would land inside
	//    the mark silhouette mask, drop it (the letter is "behind" the
	//    mark there). Skip index 0 which is the now-parametric mark.
	//    Letters live in grid rows; vpad/pad center them in the canvas.
	for i := 1; i < len(m.regions); i++ {
		reg := m.regions[i]
		offset := int(math.Round(reg.Pos)) - reg.StartCol + pad
		for r := 0; r < gridH; r++ {
			row := m.grid[r]
			dstR := r + vpad
			lo, hi := maskLo[dstR], maskHi[dstR]
			for srcC := reg.StartCol; srcC < reg.EndCol && srcC < len(row); srcC++ {
				dstC := srcC + offset
				if dstC < 0 || dstC >= outW {
					continue
				}
				if lo >= 0 && dstC >= lo && dstC <= hi {
					continue // hidden behind the mark silhouette
				}
				if row[srcC].lit {
					composed[dstR][dstC] = row[srcC]
				}
			}
		}
	}

	// 4) Paint the parametric mark on top so the diamond outline always
	//    sits in front of the chomped letter.
	for r, row := range markCells {
		dstR := r + markYOff
		if dstR < 0 || dstR >= composedH {
			continue
		}
		for c, cell := range row {
			dstC := c + pad
			if cell.lit && dstC >= 0 && dstC < outW {
				composed[dstR][dstC] = cell
			}
		}
	}

	// Render composed grid with the shimmer sweep.
	return m.shimmerCompose(composed, cols, outW)
}

// shimmerCompose serializes a composed logo grid to a styled string,
// applying the moving highlight band. Shimmer is measured in content
// space (x - contentPad) so its cadence is unaffected by the wider
// (full-screen) canvas. Shared by the chomp and scan renderers.
func (m setupModel) shimmerCompose(composed [][]logoCell, cols, outW int) string {
	contentPad := (outW - cols) / 2
	n := len(m.styles)
	bandW := float64(cols) / 4.0
	if bandW < 4 {
		bandW = 4
	}
	span := float64(cols) + 2*bandW
	center := math.Mod(float64(m.frame)*0.5, span) - bandW

	var b strings.Builder
	for _, row := range composed {
		for x, cell := range row {
			if !cell.lit {
				b.WriteString(cell.glyph)
				continue
			}
			if !m.colorOn || n == 0 {
				b.WriteString(cell.glyph)
				continue
			}
			d := math.Abs(float64(x-contentPad) - center)
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
			b.WriteString(m.styles[idx].Render(cell.glyph))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderScan composites a frame of the scan effect: the still wordmark
// (with already-scanned letters wiped away on the left), and the open-jaw
// mark gliding right over it. Flying ASCII glyphs are overlaid by View().
func (m setupModel) renderScan() string {
	cols := m.cols
	gridH := len(m.grid)
	if gridH == 0 || cols == 0 || len(m.regions) < 2 {
		return ""
	}
	outW := m.width
	if outW < cols {
		outW = cols
	}
	pad := (outW - cols) / 2

	drawMark(m.chompMarkScratch, m.chompMarkDims, m.chompGap)
	markCells := dotGridToCells(m.chompMarkScratch)
	markCellW := 0
	for _, row := range markCells {
		if len(row) > markCellW {
			markCellW = len(row)
		}
	}

	composedH, vpad, markYOff, _ := m.chompLayout()
	composed := make([][]logoCell, composedH)
	for r := range composed {
		composed[r] = make([]logoCell, outW)
		for c := range composed[r] {
			composed[r][c] = logoCell{glyph: " "}
		}
	}

	// 1) Still wordmark letters. Cells left of the mark's sweep column
	//    (wipeCut) are "scanned away"; the mark's home region is skipped
	//    (the mark itself has glided off of it). During the return the
	//    whole word stays consumed — it must NOT un-wipe as the mark
	//    glides back left; the letters only reappear via the fly-in once
	//    the mark is home.
	wipeCut := int(math.Round(m.scanMarkX))
	if m.scanSub == scanSubReturn {
		wipeCut = cols
	}
	firstLetter := m.regions[1].StartCol
	for r := 0; r < gridH; r++ {
		dstR := r + vpad
		if dstR < 0 || dstR >= composedH {
			continue
		}
		row := m.grid[r]
		for c := firstLetter; c < len(row); c++ {
			if c < wipeCut || !row[c].lit {
				continue
			}
			dstC := c + pad
			if dstC >= 0 && dstC < outW {
				composed[dstR][dstC] = row[c]
			}
		}
	}

	// 2) The gliding mark on top, ink-centered on scanMarkX.
	markLeft := int(math.Round(m.scanMarkX)) - markCellW/2
	for r, row := range markCells {
		dstR := r + markYOff
		if dstR < 0 || dstR >= composedH {
			continue
		}
		for c, cell := range row {
			if !cell.lit {
				continue
			}
			dstC := c + markLeft + pad
			if dstC >= 0 && dstC < outW {
				composed[dstR][dstC] = cell
			}
		}
	}

	return m.shimmerCompose(composed, cols, outW)
}

func (m setupModel) renderStep() string {
	switch m.step {
	case stepRegion:
		return m.renderRegion()
	case stepKey:
		return m.renderKey()
	case stepValidating:
		return m.renderValidating()
	case stepSkill:
		return m.renderSkillPrompt()
	}
	return ""
}

func (m setupModel) renderRegion() string {
	var b strings.Builder
	b.WriteString(stHeading.Render("Where is your Extend workspace?"))
	b.WriteString("\n\n")
	for i, r := range setupRegionChoices {
		marker := "  "
		radio := "( )"
		label := r.title
		host := stDim.Render("  " + r.api)
		if i == m.cursor {
			marker = stSelected.Render("❯ ")
			radio = stSelected.Render("(•)")
			label = stSelected.Render(r.title)
		}
		fmt.Fprintf(&b, "%s%s %s%s\n", marker, radio, label, host)
	}
	return b.String()
}

func (m setupModel) renderKey() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", stHeading.Render("Region:"), m.region.title+stDim.Render("  ("+m.region.api+")"))
	b.WriteString("Create an API key in the dashboard:\n")
	fmt.Fprintf(&b, "  %s\n", stLink.Render(m.region.dashboard))
	b.WriteString(stDim.Render("  → Developers → Create new key") + "\n\n")
	b.WriteString("Paste it here (input hidden):\n")
	b.WriteString(m.input.View() + "\n")
	if m.valErr != nil {
		b.WriteString("\n" + stBad.Render("✗ "+setupValidationMessage(m.valErr)))
	}
	return b.String()
}

func (m setupModel) renderValidating() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s Validating your API key…\n\n", m.spin.View())
	b.WriteString(stDim.Render("  contacting " + m.region.api))
	return b.String()
}

func (m setupModel) renderSkillPrompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s API key validated.\n\n", stGood.Render("✓"))
	b.WriteString(stHeading.Render("Install the Extend agent skill?"))
	b.WriteString("\n")
	b.WriteString(stDim.Render("  Teaches coding agents (Claude Code, Codex, OpenCode, …) how to") + "\n")
	b.WriteString(stDim.Render("  drive the Extend CLI. Writes ~/.agents/skills/extend/SKILL.md.") + "\n\n")

	choices := []string{"Yes, install it", "No thanks"}
	for i, c := range choices {
		marker, radio, label := "  ", "( )", c
		if i == m.cursor {
			marker = stSelected.Render("❯ ")
			radio = stSelected.Render("(•)")
			label = stSelected.Render(c)
		}
		fmt.Fprintf(&b, "%s%s %s\n", marker, radio, label)
	}
	return b.String()
}

func (m setupModel) footerHint() string {
	switch m.step {
	case stepRegion:
		return "↑/↓ move · enter select · q quit"
	case stepKey:
		return "enter validate · esc back · ctrl+c quit"
	case stepValidating:
		return "please wait…"
	case stepSkill:
		return "↑/↓ choose · y/n · enter confirm"
	}
	return ""
}

// setupValidationMessage turns a validation error into a short,
// human-friendly line for the wizard.
func setupValidationMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errEmptyKey) {
		return "Please paste an API key."
	}
	if apiErr, ok := apiErrorStatus(err); ok {
		switch apiErr {
		case 401:
			return "Key rejected (401). Double-check you copied the whole key."
		case 403:
			return "Forbidden (403). If this is an org key, it may need a workspace (EXTEND_WORKSPACE_ID)."
		}
	}
	return err.Error()
}
