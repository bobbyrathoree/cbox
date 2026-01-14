// Package output provides terminal output formatting utilities.
package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/fatih/color"
)

// Console handles formatted terminal output.
type Console struct {
	out     io.Writer
	err     io.Writer
	verbose bool
	quiet   bool
	mu      sync.Mutex
}

// New creates a new Console with default settings.
func New() *Console {
	return &Console{
		out: os.Stdout,
		err: os.Stderr,
	}
}

// NewWithOptions creates a Console with custom settings.
func NewWithOptions(verbose, quiet bool) *Console {
	return &Console{
		out:     os.Stdout,
		err:     os.Stderr,
		verbose: verbose,
		quiet:   quiet,
	}
}

// Color definitions
var (
	green   = color.New(color.FgGreen).SprintFunc()
	red     = color.New(color.FgRed).SprintFunc()
	yellow  = color.New(color.FgYellow).SprintFunc()
	blue    = color.New(color.FgBlue).SprintFunc()
	cyan    = color.New(color.FgCyan).SprintFunc()
	magenta = color.New(color.FgMagenta).SprintFunc()
	bold    = color.New(color.Bold).SprintFunc()
	dim     = color.New(color.Faint).SprintFunc()
)

// ServiceColors returns a color function for a service name.
// Uses a consistent color based on service name hash.
var serviceColors = []func(a ...interface{}) string{
	cyan, magenta, yellow, blue, green,
}

// ServiceColor returns a color function for the given service name.
func ServiceColor(name string) func(a ...interface{}) string {
	// Simple hash to get consistent color
	hash := 0
	for _, c := range name {
		hash = (hash*31 + int(c)) % len(serviceColors)
	}
	return serviceColors[hash]
}

// Success prints a success message with a green checkmark.
func (c *Console) Success(format string, args ...interface{}) {
	if c.quiet {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(c.out, "%s %s\n", green("✓"), msg)
}

// Error prints an error message with a red X.
func (c *Console) Error(format string, args ...interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(c.err, "%s %s\n", red("✗"), msg)
}

// ErrorWithHint prints an error with a suggested fix.
func (c *Console) ErrorWithHint(err string, hint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(c.err, "%s %s\n", red("✗"), err)
	fmt.Fprintf(c.err, "  %s %s\n", dim("→"), hint)
}

// Warn prints a warning message with a yellow warning sign.
func (c *Console) Warn(format string, args ...interface{}) {
	if c.quiet {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(c.out, "%s %s\n", yellow("⚠"), msg)
}

// Info prints an info message.
func (c *Console) Info(format string, args ...interface{}) {
	if c.quiet {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(c.out, "%s\n", msg)
}

// Debug prints a debug message (only in verbose mode).
func (c *Console) Debug(format string, args ...interface{}) {
	if !c.verbose {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(c.out, "%s %s\n", dim("[debug]"), dim(msg))
}

// Header prints a bold header.
func (c *Console) Header(format string, args ...interface{}) {
	if c.quiet {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(c.out, "%s\n", bold(msg))
}

// ServiceLog prints a log line for a service with colored prefix.
func (c *Console) ServiceLog(service, message string) {
	if c.quiet {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	colorFn := ServiceColor(service)
	prefix := colorFn(fmt.Sprintf("[%s]", service))
	fmt.Fprintf(c.out, "%s %s\n", prefix, message)
}

// Table prints a simple table.
func (c *Console) Table(headers []string, rows [][]string) {
	if c.quiet {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	for i, h := range headers {
		fmt.Fprintf(c.out, "%-*s  ", widths[i], bold(h))
	}
	fmt.Fprintln(c.out)

	// Print rows
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				fmt.Fprintf(c.out, "%-*s  ", widths[i], cell)
			}
		}
		fmt.Fprintln(c.out)
	}
}

// Box prints content in a box (for dev mode display).
func (c *Console) Box(lines []string) {
	if c.quiet {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find max width
	maxWidth := 0
	for _, line := range lines {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}
	maxWidth += 4 // padding

	// Print box
	border := strings.Repeat("─", maxWidth)
	fmt.Fprintf(c.out, "  ┌%s┐\n", border)
	for _, line := range lines {
		padding := maxWidth - len(line) - 2
		fmt.Fprintf(c.out, "  │ %s%s │\n", line, strings.Repeat(" ", padding))
	}
	fmt.Fprintf(c.out, "  └%s┘\n", border)
}

// Newline prints an empty line.
func (c *Console) Newline() {
	if c.quiet {
		return
	}
	fmt.Fprintln(c.out)
}

// StatusLine prints a status with colored indicator.
func (c *Console) StatusLine(name, status, ports, health string) {
	var statusColor func(a ...interface{}) string
	switch strings.ToLower(status) {
	case "running":
		statusColor = green
	case "stopped", "exited":
		statusColor = red
	case "starting", "created":
		statusColor = yellow
	default:
		statusColor = dim
	}

	var healthColor func(a ...interface{}) string
	switch strings.ToLower(health) {
	case "healthy":
		healthColor = green
	case "unhealthy":
		healthColor = red
	case "starting":
		healthColor = yellow
	default:
		healthColor = dim
	}

	fmt.Printf("%-12s %-12s %-20s %s\n",
		name,
		statusColor(status),
		ports,
		healthColor(health),
	)
}
