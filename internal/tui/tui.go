package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/walt-verweij/herdr-auto-resume/internal/coordinator"
	runtimeapi "github.com/walt-verweij/herdr-auto-resume/internal/runtime"
	tmuxadapter "github.com/walt-verweij/herdr-auto-resume/internal/runtime/tmux"
)

const pollInterval = 3 * time.Second

// Colors - bold, high-contrast palette
var (
	accentCyan   = lipgloss.Color("#00ffff") // Bright cyan
	accentPurple = lipgloss.Color("#bd93f9") // Soft purple
	brightWhite  = lipgloss.Color("#f8f8f2") // Off-white
	mutedGray    = lipgloss.Color("#6272a4") // Muted blue-gray
	borderColor  = lipgloss.Color("#44475a") // Dark purple-gray
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentCyan)

	versionStyle = lipgloss.NewStyle().
			Foreground(accentPurple)

	headerStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			MarginBottom(1)

	mainPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2)

	dimTextStyle = lipgloss.NewStyle().
			Foreground(mutedGray)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5555")).
			Bold(true)
)

// Messages
type layoutUpdateMsg struct {
	panes []runtimeapi.Pane
	err   error
}

type pollTickMsg time.Time

type initMsg struct {
	ownPaneID string
	panes     []runtimeapi.Pane
	err       error
}

type Model struct {
	version        string
	width          int
	height         int
	rt             *tmuxadapter.Adapter
	coord          *coordinator.Coordinator
	panes          []runtimeapi.Pane
	selectedPaneID string
	ownPaneID      string // The pane running autoclaude (excluded from detection)
	testPattern    string
	dryRun         bool
	err            error
	errTime        time.Time // When the error occurred (for auto-clear)
	showHelp       bool      // Whether to show the help overlay
}

func New(version, testPattern string, rt *tmuxadapter.Adapter, dryRun bool) Model {
	return Model{
		version:     version,
		rt:          rt,
		testPattern: testPattern,
		dryRun:      dryRun,
		width:       80,
		height:      24,
		coord:       coordinator.New(rt, coordinator.Config{TestPattern: testPattern, DryRun: dryRun}),
	}
}

func (m Model) Init() tea.Cmd {
	return m.doInit
}

func (m Model) doInit() tea.Msg {
	ownPaneID, err := m.rt.SelfPaneID()
	if err != nil {
		return initMsg{err: err}
	}

	panes, err := m.rt.ListPanes()
	if err != nil {
		return initMsg{ownPaneID: ownPaneID, err: err}
	}

	return initMsg{ownPaneID: ownPaneID, panes: panes}
}

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return pollTickMsg(t)
	})
}

func (m Model) fetchLayoutCmd() tea.Cmd {
	return func() tea.Msg {
		panes, err := m.rt.ListPanes()
		return layoutUpdateMsg{panes: panes, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If help is shown, any key dismisses it
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "h", "?":
			m.showHelp = true
		case "left":
			m.moveSelection(runtimeapi.DirLeft)
		case "right":
			m.moveSelection(runtimeapi.DirRight)
		case "up":
			m.moveSelection(runtimeapi.DirUp)
		case "down":
			m.moveSelection(runtimeapi.DirDown)
		case "tab":
			m.cycleMode()
		case "a":
			m.enableAll()
		case "n":
			m.disableAll()
		case "r":
			m.pollPanes()
			return m, m.fetchLayoutCmd()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case initMsg:
		if msg.err != nil {
			m.err = msg.err
			m.errTime = time.Now()
			return m, nil
		}
		m.ownPaneID = msg.ownPaneID
		m.coord = coordinator.New(m.rt, coordinator.Config{
			OwnPaneID:   m.ownPaneID,
			TestPattern: m.testPattern,
			DryRun:      m.dryRun,
		})
		m.updateLayout(msg.panes)
		m.pollPanes() // Poll immediately
		return m, tickCmd()

	case layoutUpdateMsg:
		if msg.err != nil {
			m.err = msg.err
			m.errTime = time.Now()
		} else {
			m.updateLayout(msg.panes)
		}

	case pollTickMsg:
		// Clear errors after 10 seconds
		if m.err != nil && time.Since(m.errTime) > 10*time.Second {
			m.err = nil
		}
		m.pollPanes()
		return m, tea.Batch(m.fetchLayoutCmd(), tickCmd())
	}

	return m, nil
}

func (m *Model) pollPanes() {
	if m.coord != nil {
		m.coord.Poll()
	}
}

func (m *Model) updateLayout(panes []runtimeapi.Pane) {
	m.panes = append([]runtimeapi.Pane(nil), panes...)
	if m.coord != nil {
		m.coord.SetPanes(panes)
	}
	if len(panes) > 0 && runtimeapi.PaneByID(panes, m.selectedPaneID) == nil {
		m.selectedPaneID = panes[0].ID
	}
}

func (m *Model) moveSelection(dir runtimeapi.Direction) {
	if m.coord == nil {
		return
	}

	current := runtimeapi.PaneByID(m.panes, m.selectedPaneID)
	if current == nil {
		return
	}

	next := runtimeapi.PaneInDirection(m.panes, current, dir)
	if next != nil {
		m.selectedPaneID = next.ID
	}
}

func (m *Model) cycleMode() {
	if m.coord != nil {
		m.coord.ToggleMode(m.selectedPaneID)
	}
}

func (m *Model) enableAll() {
	if m.coord != nil {
		m.coord.EnableAll()
	}
}

func (m *Model) disableAll() {
	if m.coord != nil {
		m.coord.DisableAll()
	}
}

func (m Model) View() string {
	// Show help overlay if active
	if m.showHelp {
		return m.renderHelp()
	}

	// Header with title and version
	title := titleStyle.Render("autoclaude")
	version := versionStyle.Render(fmt.Sprintf("v%s", m.version))
	headerWidth := m.width - 4
	if headerWidth < 20 {
		headerWidth = 20
	}
	// Place title left, version right
	titleLen := lipgloss.Width(title)
	versionLen := lipgloss.Width(version)
	spacerLen := headerWidth - titleLen - versionLen
	if spacerLen < 1 {
		spacerLen = 1
	}
	spacer := lipgloss.NewStyle().Width(spacerLen).Render("")
	header := headerStyle.Render(title + spacer + version)

	// Calculate main pane dimensions
	mainWidth := m.width - 4
	if mainWidth < 10 {
		mainWidth = 10
	}
	mainHeight := m.height - 7 // Account for header + 2-line footer + margins
	if mainHeight < 3 {
		mainHeight = 3
	}

	states := []coordinator.PaneState(nil)
	if m.coord != nil {
		states = m.coord.Snapshot()
	}

	// Render content
	var content string
	if m.err != nil {
		content = errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	} else if len(states) == 0 {
		content = dimTextStyle.Render("No panes found")
	} else {
		// Render the ASCII layout
		layoutWidth := mainWidth - 4 // Account for padding
		layoutHeight := mainHeight - 2
		content = renderLayout(states, m.selectedPaneID, layoutWidth, layoutHeight)
	}

	mainPane := mainPaneStyle.
		Width(mainWidth).
		Height(mainHeight).
		Render(content)

	// Footer with selected pane status (left) and help (right)
	var statusText string

	// Show "continue sent" message for 20 seconds after sending
	if m.coord != nil {
		if action, ok := m.coord.LastAction(); ok && time.Since(action.Time) < 20*time.Second {
			statusText = lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c")).Bold(true).Render("↳ continue sent!")
		} else {
			for _, state := range states {
				if state.Pane.ID != m.selectedPaneID || !state.HasClaudeCode {
					continue
				}
				// Always show auto-continue status first
				if state.Mode == coordinator.ModeAuto {
					statusText = lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Render("● Auto-continue enabled")
					// Add rate limit info on same line if applicable (only when auto mode)
					if state.IsRateLimited {
						if state.ContinueSent {
							statusText += lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c")).Bold(true).Render(" continue sent")
						} else if state.RateLimitResets != "" {
							statusText += errorStyle.Render(" resets " + state.RateLimitResets)
						} else {
							statusText += errorStyle.Render(" rate limited")
						}
					}
				} else {
					statusText = dimTextStyle.Render("○ Auto-continue disabled")
				}
				break
			}
		}
	}

	helpText := dimTextStyle.Render("←↑↓→ nav • tab toggle • a on • n off • r refresh • h help • q quit")

	// Footer: status on first line, help on second line (both left-aligned)
	var footer string
	if statusText != "" {
		footer = "  " + statusText + "\n  " + helpText
	} else {
		footer = "  " + helpText
	}

	// Compose the full view
	return lipgloss.JoinVertical(lipgloss.Left, header, mainPane, footer)
}

func (m Model) renderHelp() string {
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentCyan).
		Padding(1, 2).
		Width(m.width - 4)

	titleLine := titleStyle.Render("autoclaude") + " " + versionStyle.Render(fmt.Sprintf("v%s", m.version))

	helpContent := `
Monitors tmux panes running Claude Code and automatically
sends "continue" when rate limits reset.

` + lipgloss.NewStyle().Bold(true).Foreground(accentCyan).Render("KEYS") + `

  ←↑↓→      Navigate between panes
  tab       Toggle auto-continue for selected pane
  a         Enable auto-continue for all Claude Code panes
  n         Disable auto-continue for all Claude Code panes
  r         Refresh pane layout
  h / ?     Show this help
  q         Quit

` + lipgloss.NewStyle().Bold(true).Foreground(accentCyan).Render("PANE COLORS") + `

  ` + lipgloss.NewStyle().Foreground(claudeOrange).Render("Orange") + `      Claude Code pane (auto-continue off)
  ` + lipgloss.NewStyle().Foreground(autoGreen).Render("Green") + `       Claude Code pane (auto-continue on)
  ` + lipgloss.NewStyle().Foreground(rateLimitRed).Render("Red") + `         Rate limited (waiting for reset)
  ` + lipgloss.NewStyle().Foreground(accentCyan).Render("Cyan") + `        Selected pane

` + lipgloss.NewStyle().Bold(true).Foreground(accentCyan).Render("HOW IT WORKS") + `

  When a Claude Code pane shows a rate limit message like
  "limit reached ∙ resets Xpm" or "You've hit your limit",
  autoclaude waits for that time to pass, then sends:
  Escape → "continue" → Enter

  Polling occurs every 3 seconds.

` + dimTextStyle.Render("Made by Henry Stanley (henrystanley.com)") + `
` + dimTextStyle.Render("Built with Claude Code")

	footer := dimTextStyle.Render("Press any key to close")

	return lipgloss.JoinVertical(lipgloss.Left,
		"",
		helpStyle.Render(titleLine+helpContent),
		"  "+footer,
	)
}
