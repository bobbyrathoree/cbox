package output

import (
	"fmt"
	"os"
	"time"

	"github.com/briandowns/spinner"
)

// Spinner provides a progress indicator for long-running operations.
type Spinner struct {
	s       *spinner.Spinner
	message string
	quiet   bool
}

// NewSpinner creates a new spinner with the given message.
func NewSpinner(message string, quiet bool) *Spinner {
	if quiet {
		return &Spinner{quiet: true, message: message}
	}

	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " " + message
	s.Writer = os.Stdout

	return &Spinner{
		s:       s,
		message: message,
	}
}

// Start begins the spinner animation.
func (sp *Spinner) Start() {
	if sp.quiet || sp.s == nil {
		return
	}
	sp.s.Start()
}

// Stop stops the spinner animation.
func (sp *Spinner) Stop() {
	if sp.quiet || sp.s == nil {
		return
	}
	sp.s.Stop()
}

// Success stops the spinner and shows a success message.
func (sp *Spinner) Success(message string) {
	sp.Stop()
	if sp.quiet {
		return
	}
	fmt.Printf("%s %s\n", green("✓"), message)
}

// Fail stops the spinner and shows an error message.
func (sp *Spinner) Fail(message string) {
	sp.Stop()
	fmt.Fprintf(os.Stderr, "%s %s\n", red("✗"), message)
}

// Update changes the spinner message.
func (sp *Spinner) Update(message string) {
	sp.message = message
	if sp.quiet || sp.s == nil {
		return
	}
	sp.s.Suffix = " " + message
}

// MultiSpinner manages multiple concurrent spinners for parallel operations.
type MultiSpinner struct {
	console *Console
	tasks   map[string]*taskStatus
	quiet   bool
}

type taskStatus struct {
	name    string
	status  string
	done    bool
	success bool
}

// NewMultiSpinner creates a manager for multiple concurrent tasks.
func NewMultiSpinner(console *Console, quiet bool) *MultiSpinner {
	return &MultiSpinner{
		console: console,
		tasks:   make(map[string]*taskStatus),
		quiet:   quiet,
	}
}

// AddTask adds a task to track.
func (ms *MultiSpinner) AddTask(name, initialStatus string) {
	ms.tasks[name] = &taskStatus{
		name:   name,
		status: initialStatus,
	}
}

// UpdateTask updates a task's status.
func (ms *MultiSpinner) UpdateTask(name, status string) {
	if task, ok := ms.tasks[name]; ok {
		task.status = status
	}
}

// CompleteTask marks a task as complete.
func (ms *MultiSpinner) CompleteTask(name string, success bool, finalStatus string) {
	if task, ok := ms.tasks[name]; ok {
		task.done = true
		task.success = success
		task.status = finalStatus
	}
	ms.printTask(name)
}

func (ms *MultiSpinner) printTask(name string) {
	if ms.quiet {
		return
	}
	task := ms.tasks[name]
	if task == nil {
		return
	}

	if task.success {
		ms.console.Success("%s: %s", task.name, task.status)
	} else if task.done {
		ms.console.Error("%s: %s", task.name, task.status)
	}
}

// Progress represents a progress bar for operations with known steps.
type Progress struct {
	total   int
	current int
	message string
	console *Console
}

// NewProgress creates a new progress tracker.
func NewProgress(total int, message string, console *Console) *Progress {
	return &Progress{
		total:   total,
		message: message,
		console: console,
	}
}

// Increment advances the progress by one.
func (p *Progress) Increment(stepMessage string) {
	p.current++
	if p.console.quiet {
		return
	}
	fmt.Printf("\r%s [%d/%d] %s", p.message, p.current, p.total, stepMessage)
	if p.current == p.total {
		fmt.Println()
	}
}

// Complete marks the progress as done.
func (p *Progress) Complete() {
	if p.console.quiet {
		return
	}
	fmt.Printf("\r%s %s [%d/%d]\n", green("✓"), p.message, p.total, p.total)
}
