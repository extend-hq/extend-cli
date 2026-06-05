package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/extend-hq/extend-cli/internal/config"
)

// setupStep is the wizard's linear state machine.
type setupStep int

const (
	stepRegion setupStep = iota
	stepKey
	stepValidating
	stepDone
)

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
}

type logoTickMsg struct{}
type validatedMsg struct{ err error }

func logoTickCmd() tea.Cmd {
	return tea.Tick(70*time.Millisecond, func(time.Time) tea.Msg { return logoTickMsg{} })
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
	styles []lipgloss.Style
	width  int
	height int
	cols   int
	grid   [][]logoCell
	frame  int
	reveal int

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
	ti.Width = 46

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
	m.setWidth(96)
	return m
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
	m.cols = cols
	m.grid = buildLogoGrid(cols)
	if iw := cols - 6; iw > 12 {
		if iw > 60 {
			iw = 60
		}
		m.input.Width = iw
	}
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
		if m.grid != nil && m.reveal < m.cols {
			step := m.cols / 18
			if step < 2 {
				step = 2
			}
			m.reveal += step
			if m.reveal > m.cols {
				m.reveal = m.cols
			}
		}
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
		path, err := config.Save(config.File{Region: m.region.id, APIKey: m.apiKey})
		m.result = &setupResult{region: m.region, apiKey: m.apiKey, path: path, saveErr: err}
		m.step = stepDone
		m.quitting = true
		return m, tea.Quit

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.step == stepKey {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m setupModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m setupModel) View() string {
	if m.quitting {
		return ""
	}

	logo := m.renderLogo()
	tagline := stTagline.Render("Document AI platform · command-line setup")
	divider := stDivider.Render(strings.Repeat("─", m.cols))

	body := lipgloss.JoinVertical(
		lipgloss.Center,
		logo,
		"",
		tagline,
		divider,
	)

	content := m.renderStep()
	content = lipgloss.NewStyle().Width(m.cols).Height(8).Render(content)

	footer := stDim.Render(m.footerHint())

	inner := lipgloss.JoinVertical(
		lipgloss.Center,
		body,
		"",
		content,
		"",
		footer,
	)

	boxed := stBox.Render(inner)

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, boxed)
	}
	return boxed
}

func (m setupModel) renderLogo() string {
	if len(m.grid) > 0 {
		anim := logoAnim{grid: m.grid, cols: m.cols, styles: m.styles, colorOn: m.colorOn}
		reveal := m.reveal
		if reveal > m.cols {
			reveal = m.cols
		}
		if s := anim.render(m.frame, reveal); s != "" {
			return s
		}
	}
	// Fallback when the image can't be decoded.
	return stHeading.Render("E X T E N D")
}

func (m setupModel) renderStep() string {
	switch m.step {
	case stepRegion:
		return m.renderRegion()
	case stepKey:
		return m.renderKey()
	case stepValidating:
		return m.renderValidating()
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
	b.WriteString(stDim.Render("  → Settings → API Keys → Create key") + "\n\n")
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

func (m setupModel) footerHint() string {
	switch m.step {
	case stepRegion:
		return "↑/↓ move · enter select · q quit"
	case stepKey:
		return "enter validate · esc back · ctrl+c quit"
	case stepValidating:
		return "please wait…"
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
