package tui

import (
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wrap"
)

type Pager struct {
	Width  int
	Height int

	// YOffset is the vertical scroll position.
	YOffset int

	// YPosition is the position of the pager in relation to the terminal
	// window. It's used in high performance rendering.
	YPosition int

	lines []string
}

// AtTop returns whether or not the pager is in the very top position.
func (p *Pager) AtTop() bool {
	return p.YOffset <= 0
}

// AtBottom returns whether or not the pager is at or past the very bottom
// position.
func (p *Pager) AtBottom() bool {
	return p.YOffset >= len(p.lines)-1-p.Height
}

// PastBottom returns whether or not the pager is scrolled beyond the last
// line. This can happen when adjusting the pager height.
func (p *Pager) PastBottom() bool {
	return p.YOffset > len(p.lines)-1-p.Height
}

// ScrollPercent returns the amount scrolled as a float between 0 and 1.
func (p *Pager) ScrollPercent() float64 {
	if p.Height >= len(p.lines) {
		return 1.0
	}
	y := float64(p.YOffset)
	h := float64(p.Height)
	t := float64(len(p.lines) - 1)
	v := y / (t - h)
	return math.Max(0.0, math.Min(1.0, v))
}

// SetContent set the pager's text content. For high performance rendering the
// Sync command should also be called.
func (p *Pager) SetContent(s string) {
	p.lines = strings.Split(wrap.String(s, p.Width), "\n")
}

// Return the lines that should currently be visible in the pager
func (p *Pager) visibleLines() (lines []string) {
	if len(p.lines) > 0 {
		top := max(0, p.YOffset)
		bottom := clamp(p.YOffset+p.Height, top, len(p.lines))
		lines = p.lines[top:bottom]
	}
	return lines
}

// ViewDown moves the view down by the number of lines in the pager
func (p *Pager) ViewDown() {
	if p.AtBottom() {
		return
	}
	p.YOffset = min(
		p.YOffset+p.Height,      // target
		len(p.lines)-1-p.Height, // fallback
	)
}

// ViewUp moves the view up by one height of the pager
func (p *Pager) ViewUp() {
	if p.AtTop() {
		return
	}
	p.YOffset = max(
		p.YOffset-p.Height, // target
		0,                  // fallback
	)
}

// HalfViewDown moves the view down by half the height of the pager
func (p *Pager) HalfViewDown() {
	if p.AtBottom() {
		return
	}
	p.YOffset = min(
		p.YOffset+p.Height/2,    // target
		len(p.lines)-1-p.Height, // fallback
	)
}

// HalfViewUp moves the view up by half the height of the pager
func (p *Pager) HalfViewUp() {
	if p.AtTop() {
		return
	}
	p.YOffset = max(
		p.YOffset-p.Height/2, // target
		0,                    // fallback
	)
}

// LineDown moves the view down by the given number of lines.
func (p *Pager) LineDown(n int) {
	if p.AtBottom() || n == 0 {
		return
	}

	// Make sure the number of lines by which we're going to scroll isn't
	// greater than the number of lines we actually have left before we reach
	// the bottom.
	//
	// number of lines - pager bottom edge
	maxDelta := (len(p.lines) - 1) - (p.YOffset + p.Height)
	n = min(n, maxDelta)
	p.YOffset = min(
		p.YOffset+n,             // target
		len(p.lines)-1-p.Height, // fallback
	)
}

// LineUp moves the view down by the given number of lines. Returns the new
// lines to show.
func (p *Pager) LineUp(n int) {
	if p.AtTop() || n == 0 {
		return
	}

	// Make sure the number of lines by which we're going to scroll isn't
	// greater than the number of lines we are from the top.
	n = min(n, p.YOffset)
	p.YOffset = max(p.YOffset-n, 0)
}

// GotoTop sets the pager to the top position.
func (p *Pager) GotoTop() {
	if p.AtTop() {
		return
	}
	p.YOffset = 0
}

// GotoTop sets the pager to the bottom position.
func (p *Pager) GotoBottom() {
	p.YOffset = max(len(p.lines)-1-p.Height, 0)
}

// Update runs the update loop with default keybindings similar to popular
// pagers. To define your own keybindings use the methods on Pager (i.e.
// Pager.LineDown()) and define your own update function.
func (p *Pager) Update(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		// Down one page
		case "pgdown", " ", "f":
			p.ViewDown()
		// Up one page
		case "pgup", "b":
			p.ViewUp()
		// Down half page
		case "d", "ctrl+d":
			p.HalfViewDown()
		// Up half page
		case "u", "ctrl+u":
			p.HalfViewUp()
		// Down one line
		case "down", "j":
			p.LineDown(1)
		// Up one line
		case "up", "k":
			p.LineUp(1)
		}
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			p.LineUp(3)
		case tea.MouseWheelDown:
			p.LineDown(3)
		}
	}
}

// View renders the pager into a string.
func (p *Pager) View() string {
	lines := p.visibleLines()

	// Fill empty space with newlines
	extraLines := ""
	if len(lines) < p.Height {
		extraLines = strings.Repeat("\n", p.Height-len(lines))
	}

	return strings.Join(lines, "\n") + extraLines
}

func clamp(v, low, high int) int {
	return min(high, max(low, v))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
