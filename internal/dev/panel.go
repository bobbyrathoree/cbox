package dev

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Panel renders a persistent 2-line status bar at the bottom of the terminal.
type Panel struct {
	mu           sync.Mutex
	enabled      bool // false if not a TTY
	services     map[string]ServiceStatus
	lastAction   string
	lastActionAt time.Time
	width        int
}

// ServiceStatus holds display info for one service.
type ServiceStatus struct {
	Name   string
	Status string // "Running", "Stopped", "Building"
	Memory string // e.g., "120MB"
	Port   int
}

// NewPanel creates a new panel. It auto-detects TTY.
func NewPanel() *Panel {
	p := &Panel{
		services: make(map[string]ServiceStatus),
	}

	// Only enable if stdout is a real terminal
	if term.IsTerminal(int(os.Stdout.Fd())) {
		w, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err == nil && w > 0 {
			p.enabled = true
			p.width = w
		}
	}

	return p
}

// IsEnabled returns true if the panel is active (TTY detected).
func (p *Panel) IsEnabled() bool {
	return p.enabled
}

// SetupScrollRegion configures the terminal to leave the bottom 2 lines for the panel.
func (p *Panel) SetupScrollRegion() {
	if !p.enabled {
		return
	}
	_, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || h < 5 {
		p.enabled = false
		return
	}
	// Set scroll region to exclude bottom 2 lines
	fmt.Fprintf(os.Stdout, "\033[1;%dr", h-2)
	// Move cursor to top of scroll region
	fmt.Fprintf(os.Stdout, "\033[1;1H")
	// Draw initial panel
	p.Redraw()
}

// TeardownScrollRegion restores the terminal to normal.
func (p *Panel) TeardownScrollRegion() {
	if !p.enabled {
		return
	}
	// Reset scroll region to full terminal
	fmt.Fprintf(os.Stdout, "\033[r")
	// Move cursor to bottom
	fmt.Fprintf(os.Stdout, "\033[999;1H\n")
}

// UpdateService updates the status of a service.
func (p *Panel) UpdateService(name, status, memory string, port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.services[name] = ServiceStatus{
		Name:   name,
		Status: status,
		Memory: memory,
		Port:   port,
	}
}

// SetAction sets the last watcher action message.
func (p *Panel) SetAction(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastAction = msg
	p.lastActionAt = time.Now()
}

// Redraw renders the panel at the bottom of the terminal.
func (p *Panel) Redraw() {
	if !p.enabled {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	_, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return
	}
	w := p.width
	if nw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		w = nw
	}

	// Save cursor position
	fmt.Fprintf(os.Stdout, "\033[s")

	// Line 1: Service status pills
	line1 := p.renderServiceLine(w)
	fmt.Fprintf(os.Stdout, "\033[%d;1H\033[2K%s", h-1, line1)

	// Line 2: Last action
	line2 := p.renderActionLine(w)
	fmt.Fprintf(os.Stdout, "\033[%d;1H\033[2K%s", h, line2)

	// Restore cursor position
	fmt.Fprintf(os.Stdout, "\033[u")
}

func (p *Panel) renderServiceLine(width int) string {
	if len(p.services) == 0 {
		return p.centerPad("--- waiting for services ---", width)
	}

	var pills []string
	for _, svc := range p.services {
		pill := fmt.Sprintf("[%s: %s", svc.Name, svc.Status)
		if svc.Memory != "" {
			pill += " " + svc.Memory
		}
		if svc.Port > 0 {
			pill += fmt.Sprintf(" :%d", svc.Port)
		}
		pill += "]"
		pills = append(pills, pill)
	}

	content := strings.Join(pills, " ")
	padding := width - len(content) - 6
	if padding < 2 {
		padding = 2
	}
	left := padding / 2
	right := padding - left

	return fmt.Sprintf("%s %s %s", strings.Repeat("-", left), content, strings.Repeat("-", right))
}

func (p *Panel) renderActionLine(width int) string {
	if p.lastAction == "" {
		return p.centerPad("Watching for changes...", width)
	}

	// Fade action after 10 seconds
	if time.Since(p.lastActionAt) > 10*time.Second {
		return p.centerPad("Watching for changes...", width)
	}

	msg := p.lastAction
	if len(msg) > width-2 {
		msg = msg[:width-5] + "..."
	}
	return msg
}

func (p *Panel) centerPad(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	pad := (width - len(s)) / 2
	return strings.Repeat(" ", pad) + s
}
