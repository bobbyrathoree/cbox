// Package dashboard provides a TUI for monitoring services.
package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/orchestrator"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252"))

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("255"))

	runningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	stoppedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	healthyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	unhealthyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))
)

// ServiceInfo holds display info for a service
type ServiceInfo struct {
	Name   string
	Status string
	Ports  string
	Health string
	CPU    string
	Memory string
	NetIO  string
}

// Model is the main TUI model
type Model struct {
	projectName string
	services    []ServiceInfo
	selected    int
	width       int
	height      int
	err         error
	quitting    bool

	// Dependencies
	orch    *orchestrator.Orchestrator
	docker  *runtime.Docker
	cfg     *config.Config
	console *output.Console
}

// New creates a new dashboard model
func New(cfg *config.Config, console *output.Console) Model {
	orch := orchestrator.New(cfg, console)
	docker := runtime.New(console)
	return Model{
		projectName: cfg.Project.Name,
		orch:        orch,
		docker:      docker,
		cfg:         cfg,
		console:     console,
		services:    []ServiceInfo{},
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle(fmt.Sprintf("cbox - %s", m.projectName)),
		pollServices(m.orch, m.docker, m.projectName),
	)
}

// pollServicesMsg carries service status updates
type pollServicesMsg struct {
	services []ServiceInfo
	err      error
}

// pollServices polls service status and live resource metrics
func pollServices(orch *orchestrator.Orchestrator, docker *runtime.Docker, projectName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		statuses, err := orch.Ps(ctx, true)
		if err != nil {
			return pollServicesMsg{err: err}
		}

		// Fetch live container stats
		labels := map[string]string{"cbox.project": projectName}
		if ns := orch.GetNamespace(); ns != "" {
			labels["cbox.namespace"] = ns
		}
		statsMap, _ := docker.GetContainerStats(ctx, labels)

		var services []ServiceInfo
		for _, s := range statuses {
			svc := ServiceInfo{
				Name:   s.Name,
				Status: parseStatus(s.Status),
				Ports:  strings.Join(s.Ports, ", "),
				Health: s.Health,
				CPU:    "-",
				Memory: "-",
				NetIO:  "-",
			}
			// Merge live stats if available
			for containerName, stats := range statsMap {
				if strings.HasSuffix(containerName, "_"+s.Name) {
					svc.CPU = stats.CPU
					svc.Memory = stats.Memory
					svc.NetIO = stats.NetIO
					break
				}
			}
			services = append(services, svc)
		}

		return pollServicesMsg{services: services}
	}
}

// parseStatus extracts status from docker status string
func parseStatus(status string) string {
	lower := strings.ToLower(status)
	if strings.Contains(lower, "up") {
		return "running"
	}
	if strings.Contains(lower, "exited") {
		return "stopped"
	}
	if strings.Contains(lower, "created") {
		return "created"
	}
	return status
}

// tickMsg triggers periodic refresh
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "j", "down":
			if m.selected < len(m.services)-1 {
				m.selected++
			}

		case "k", "up":
			if m.selected > 0 {
				m.selected--
			}

		case "r":
			// Restart selected service
			if m.selected < len(m.services) {
				svc := m.services[m.selected]
				return m, restartService(m.docker, m.projectName, m.orch.GetNamespace(), svc.Name)
			}

		case "s":
			// Stop selected service
			if m.selected < len(m.services) {
				svc := m.services[m.selected]
				return m, stopService(m.docker, m.projectName, m.orch.GetNamespace(), svc.Name)
			}

		case "u":
			// Start/up selected service
			if m.selected < len(m.services) {
				svc := m.services[m.selected]
				return m, startService(m.orch, m.cfg, svc.Name)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case pollServicesMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.services = msg.services
			// Adjust selection if needed
			if m.selected >= len(m.services) && len(m.services) > 0 {
				m.selected = len(m.services) - 1
			}
		}
		return m, tickCmd()

	case tickMsg:
		return m, pollServices(m.orch, m.docker, m.projectName)

	case actionCompleteMsg:
		// Refresh after action
		return m, pollServices(m.orch, m.docker, m.projectName)
	}

	return m, nil
}

// actionCompleteMsg signals an action completed
type actionCompleteMsg struct {
	action  string
	service string
	err     error
}

func restartService(docker *runtime.Docker, projectName string, namespace string, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		containerName := projectName + "_" + name
		if namespace != "" {
			containerName = namespace + "-" + projectName + "_" + name
		}
		err := docker.RestartContainer(ctx, containerName, 10*time.Second)

		return actionCompleteMsg{action: "restart", service: name, err: err}
	}
}

func stopService(docker *runtime.Docker, projectName string, namespace string, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		containerName := projectName + "_" + name
		if namespace != "" {
			containerName = namespace + "-" + projectName + "_" + name
		}
		err := docker.StopContainer(ctx, containerName, 10*time.Second)

		return actionCompleteMsg{action: "stop", service: name, err: err}
	}
}

func startService(orch *orchestrator.Orchestrator, cfg *config.Config, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		orch.Up(ctx, orchestrator.UpOptions{Services: []string{name}})

		return actionCompleteMsg{action: "start", service: name}
	}
}

// View renders the TUI
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	title := titleStyle.Render(fmt.Sprintf(" cbox - %s ", m.projectName))
	b.WriteString(title)
	b.WriteString("\n\n")

	// Services table
	if len(m.services) == 0 {
		b.WriteString(dimStyle.Render("  No services found. Run 'cbox up' first."))
		b.WriteString("\n")
	} else {
		// Header
		header := fmt.Sprintf("  %-15s %-10s %-25s %-10s %-8s %-20s %-14s",
			headerStyle.Render("SERVICE"),
			headerStyle.Render("STATUS"),
			headerStyle.Render("PORTS"),
			headerStyle.Render("HEALTH"),
			headerStyle.Render("CPU"),
			headerStyle.Render("MEMORY"),
			headerStyle.Render("NET I/O"),
		)
		b.WriteString(header)
		b.WriteString("\n")

		// Rows
		for i, svc := range m.services {
			statusStyle := dimStyle
			if svc.Status == "running" {
				statusStyle = runningStyle
			} else if svc.Status == "stopped" {
				statusStyle = stoppedStyle
			}

			healthStyle := dimStyle
			if svc.Health == "healthy" {
				healthStyle = healthyStyle
			} else if svc.Health == "unhealthy" {
				healthStyle = unhealthyStyle
			}

			row := fmt.Sprintf("  %-15s %-10s %-25s %-10s %-8s %-20s %-14s",
				svc.Name,
				statusStyle.Render(svc.Status),
				svc.Ports,
				healthStyle.Render(svc.Health),
				svc.CPU,
				svc.Memory,
				svc.NetIO,
			)

			if i == m.selected {
				row = selectedStyle.Render(row)
			}

			b.WriteString(row)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// Help
	help := helpStyle.Render("  q: quit  j/k: navigate  r: restart  s: stop  u: start")
	b.WriteString(help)
	b.WriteString("\n")

	// Error display
	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(stoppedStyle.Render(fmt.Sprintf("  Error: %s", m.err)))
		b.WriteString("\n")
	}

	return b.String()
}

// Run starts the dashboard TUI
func Run(cfg *config.Config, console *output.Console) error {
	m := New(cfg, console)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
