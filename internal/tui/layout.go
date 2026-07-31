package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator"
)

// Colors for pane rendering - bold, high-contrast
var (
	claudeOrange    = lipgloss.Color("#ffb86c") // Bright orange for Claude Code
	autoGreen       = lipgloss.Color("#50fa7b") // Bright green for auto mode
	selectedColor   = lipgloss.Color("#00ffff") // Bright cyan for selected
	unselectedColor = lipgloss.Color("#6272a4") // Muted blue-gray for unselected
	rateLimitRed    = lipgloss.Color("#ff5555") // Bright red for rate limited
)

// Box drawing characters
const (
	// Single line (unselected)
	singleTopLeft     = "┌"
	singleTopRight    = "┐"
	singleBottomLeft  = "└"
	singleBottomRight = "┘"
	singleHorizontal  = "─"
	singleVertical    = "│"

	// Double line (selected)
	doubleTopLeft     = "╔"
	doubleTopRight    = "╗"
	doubleBottomLeft  = "╚"
	doubleBottomRight = "╝"
	doubleHorizontal  = "═"
	doubleVertical    = "║"
)

// renderLayout renders the tmux pane layout as ASCII art with colors
func renderLayout(states []coordinator.PaneState, selectedID string, width, height int) string {
	if len(states) == 0 {
		return ""
	}

	// Find the bounds of all panes
	maxRight, maxBottom := 0, 0
	for _, state := range states {
		right := state.Pane.Left + state.Pane.Width
		bottom := state.Pane.Top + state.Pane.Height
		if right > maxRight {
			maxRight = right
		}
		if bottom > maxBottom {
			maxBottom = bottom
		}
	}

	if maxRight == 0 || maxBottom == 0 {
		return ""
	}

	// Create a 2D grid for rendering (stores styled strings per cell)
	grid := make([][]string, height)
	for i := range grid {
		grid[i] = make([]string, width)
		for j := range grid[i] {
			grid[i][j] = " "
		}
	}

	// Scale factors
	scaleX := float64(width-1) / float64(maxRight)
	scaleY := float64(height-1) / float64(maxBottom)

	// Draw each pane
	for _, state := range states {
		isSelected := state.Pane.ID == selectedID
		drawPane(grid, state, isSelected, scaleX, scaleY, width, height)
	}

	// Convert grid to string
	var sb strings.Builder
	for i, row := range grid {
		for _, cell := range row {
			sb.WriteString(cell)
		}
		if i < len(grid)-1 {
			sb.WriteRune('\n')
		}
	}

	return sb.String()
}

// drawPane draws a single pane on the grid with appropriate styling
func drawPane(grid [][]string, state coordinator.PaneState, selected bool, scaleX, scaleY float64, gridW, gridH int) {
	p := &state.Pane
	// Calculate scaled coordinates using round to minimize gaps between adjacent panes
	x1 := int(math.Round(float64(p.Left) * scaleX))
	y1 := int(math.Round(float64(p.Top) * scaleY))
	x2 := int(math.Round(float64(p.Left+p.Width) * scaleX))
	y2 := int(math.Round(float64(p.Top+p.Height) * scaleY))

	// Ensure minimum size
	if x2 <= x1 {
		x2 = x1 + 1
	}
	if y2 <= y1 {
		y2 = y1 + 1
	}

	// Clamp to grid bounds
	if x1 < 0 {
		x1 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if x2 >= gridW {
		x2 = gridW - 1
	}
	if y2 >= gridH {
		y2 = gridH - 1
	}

	// Determine colors based on state
	var borderColor, labelColor lipgloss.Color
	if state.IsRateLimited && state.Mode == coordinator.ModeAuto {
		// Rate limited with auto mode: red
		borderColor = rateLimitRed
		labelColor = rateLimitRed
	} else if state.HasClaudeCode && state.Mode == coordinator.ModeAuto {
		// Auto mode: green
		borderColor = autoGreen
		labelColor = autoGreen
	} else if state.HasClaudeCode {
		// Claude Code but off: orange
		borderColor = claudeOrange
		labelColor = claudeOrange
	} else if selected {
		borderColor = selectedColor
		labelColor = selectedColor
	} else {
		borderColor = unselectedColor
		labelColor = unselectedColor
	}

	// Override border color if selected
	if selected {
		borderColor = selectedColor
	}

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	labelStyle := lipgloss.NewStyle().Foreground(labelColor)

	// Choose box characters
	var tl, tr, bl, br, h, v string
	if selected {
		tl, tr, bl, br, h, v = doubleTopLeft, doubleTopRight, doubleBottomLeft, doubleBottomRight, doubleHorizontal, doubleVertical
	} else {
		tl, tr, bl, br, h, v = singleTopLeft, singleTopRight, singleBottomLeft, singleBottomRight, singleHorizontal, singleVertical
	}

	// Draw corners
	setCell(grid, x1, y1, borderStyle.Render(tl))
	setCell(grid, x2, y1, borderStyle.Render(tr))
	setCell(grid, x1, y2, borderStyle.Render(bl))
	setCell(grid, x2, y2, borderStyle.Render(br))

	// Draw horizontal lines
	styledH := borderStyle.Render(h)
	for x := x1 + 1; x < x2; x++ {
		setCell(grid, x, y1, styledH)
		setCell(grid, x, y2, styledH)
	}

	// Draw vertical lines
	styledV := borderStyle.Render(v)
	for y := y1 + 1; y < y2; y++ {
		setCell(grid, x1, y, styledV)
		setCell(grid, x2, y, styledV)
	}

	// Draw labels centered in the pane
	centerY := y1 + (y2-y1)/2
	paneWidth := x2 - x1 - 2 // Available width inside borders

	if state.HasClaudeCode {
		// CC panes: always show mode (auto/off) on center line
		statusLabel := state.Mode.String()
		drawCenteredText(grid, statusLabel, x1, x2, centerY, labelStyle)

		// Show rate limit info below mode if auto and rate limited
		nextLine := centerY + 1
		if state.IsRateLimited && state.Mode == coordinator.ModeAuto && nextLine < y2 {
			var rateLimitLabel string
			var rateLimitStyle lipgloss.Style
			if state.ContinueSent {
				rateLimitLabel = "continue sent"
				rateLimitStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c")) // Yellow
			} else if state.RateLimitResets != "" {
				rateLimitLabel = "resets " + state.RateLimitResets
				rateLimitStyle = lipgloss.NewStyle().Foreground(rateLimitRed)
			} else {
				rateLimitLabel = "rate limited"
				rateLimitStyle = lipgloss.NewStyle().Foreground(rateLimitRed)
			}
			drawCenteredText(grid, rateLimitLabel, x1, x2, nextLine, rateLimitStyle)
			nextLine++
		}

		// Show "Claude Code" label below if there's room
		if nextLine < y2 {
			titleStyle := lipgloss.NewStyle().Foreground(labelColor).Italic(true)
			drawCenteredText(grid, "Claude Code", x1, x2, nextLine, titleStyle)
		}
	} else {
		// Non-CC panes: just show title in italics
		if p.Title != "" {
			titleStyle := lipgloss.NewStyle().Foreground(labelColor).Italic(true)
			title := truncate(p.Title, paneWidth)
			drawCenteredText(grid, title, x1, x2, centerY, titleStyle)
		}
	}
}

// drawCenteredText draws text centered horizontally between x1 and x2
func drawCenteredText(grid [][]string, text string, x1, x2, y int, style lipgloss.Style) {
	textWidth := runewidth.StringWidth(text)
	labelX := x1 + (x2-x1-textWidth)/2

	if labelX > x1 && labelX+textWidth < x2 && y >= 0 && y < len(grid) {
		x := labelX
		for _, r := range text {
			w := runewidth.RuneWidth(r)
			if w > 0 {
				setCell(grid, x, y, style.Render(string(r)))
				// Wide characters (w=2) occupy 2 cells; clear the next cell
				if w == 2 {
					setCell(grid, x+1, y, "")
				}
				x += w
			}
		}
	}
}

// truncate shortens a string to fit within maxLen display width
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return runewidth.Truncate(s, maxLen, "")
	}
	return runewidth.Truncate(s, maxLen, "…")
}

// setCell safely sets a cell in the grid
func setCell(grid [][]string, x, y int, s string) {
	if y >= 0 && y < len(grid) && x >= 0 && x < len(grid[y]) {
		grid[y][x] = s
	}
}
