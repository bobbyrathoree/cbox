package dev

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bobbyrathore/cbox/internal/builder"
	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/orchestrator"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
)

// DevLoop manages the development mode lifecycle.
type DevLoop struct {
	config       *config.Config
	orchestrator *orchestrator.Orchestrator
	builder      *builder.Builder
	runtime      runtime.ContainerRuntime
	console      *output.Console
	watchers     []*Watcher
	mu           sync.Mutex

	// For signal handling and cleanup
	startedServices []string
	cleanupOnce     sync.Once

	// Background rebuild context for hot-patching
	buildCtx    context.Context
	buildCancel context.CancelFunc

	// Intelligence panel for live status display
	panel *Panel
}

// Options contains options for dev mode.
type Options struct {
	Services []string // Specific services (empty = all)
	NoSync   bool     // Rebuild instead of relying on bind mounts
}

// New creates a new DevLoop.
func New(cfg *config.Config, console *output.Console) *DevLoop {
	return &DevLoop{
		config:       cfg,
		orchestrator: orchestrator.New(cfg, console),
		builder:      builder.New(console),
		runtime:      runtime.New(console),
		console:      console,
	}
}

// Start begins the development loop.
func (d *DevLoop) Start(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Initialize the intelligence panel (auto-detects TTY)
	d.panel = NewPanel()

	// Determine services
	services := opts.Services
	if len(services) == 0 {
		for name := range d.config.Services {
			services = append(services, name)
		}
	}

	// Store services for cleanup
	d.mu.Lock()
	d.startedServices = services
	d.mu.Unlock()

	// Cleanup function that ensures containers are stopped
	cleanup := func() {
		d.cleanupOnce.Do(func() {
			// Restore terminal before printing shutdown messages
			d.panel.TeardownScrollRegion()

			d.console.Info("Stopping services...")
			d.stopWatchers()

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()

			d.orchestrator.Down(shutdownCtx, orchestrator.DownOptions{
				Timeout: 10 * time.Second,
			})
		})
	}

	// Ensure cleanup runs on normal exit
	defer cleanup()

	// Set up signal handling for SIGINT (Ctrl+C) and SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Handle signals in a goroutine
	go func() {
		sig := <-sigCh
		d.console.Newline()
		d.console.Info("Received %s, shutting down...", sig)

		// Stop listening for more signals to allow force quit on second Ctrl+C
		signal.Stop(sigCh)

		// Run cleanup
		cleanup()

		// Cancel context to stop other goroutines
		cancel()

		// Exit cleanly
		os.Exit(0)
	}()

	// Start services in dev mode
	d.console.Header("Starting %s in dev mode...", d.config.Project.Name)

	err := d.orchestrator.Up(ctx, orchestrator.UpOptions{
		Services: services,
		Build:    true,
		DevMode:  true,
		Timeout:  60 * time.Second,
	})

	if err != nil {
		return fmt.Errorf("failed to start services: %w", err)
	}

	// Set up file watchers for build services
	for _, svcName := range services {
		svc := d.config.Services[svcName]
		if !svc.IsBuildService() {
			continue
		}

		if err := d.setupWatcher(ctx, svcName, svc, opts.NoSync); err != nil {
			d.console.Warn("Failed to set up watcher for %s: %s", svcName, err)
		}
	}

	// Print dev mode info
	d.printDevInfo(services)

	// Set up the intelligence panel (persistent 2-line HUD)
	d.panel.SetupScrollRegion()

	// Start background polling for service stats
	if d.panel.IsEnabled() {
		go d.pollServiceStats(ctx)
	}

	// Stream logs from all services
	go d.streamAllLogs(ctx, services)

	// Wait for shutdown (context cancellation)
	<-ctx.Done()

	return nil
}

func (d *DevLoop) setupWatcher(ctx context.Context, svcName string, svc config.Service, noSync bool) error {
	// Get watch paths
	paths := svc.Dev.Watch.Paths
	if len(paths) == 0 {
		paths = []string{svc.Path}
	}

	// Make paths absolute
	wd, _ := os.Getwd()
	absPaths := make([]string, len(paths))
	for i, p := range paths {
		if !filepath.IsAbs(p) {
			if svc.Path != "" && svc.Path != "." {
				p = filepath.Join(svc.Path, p)
			}
			p = filepath.Join(wd, p)
		}
		absPaths[i] = p
	}

	// Create watcher
	watcher, err := NewWatcher(
		absPaths,
		svc.Dev.Watch.Ignore,
		100*time.Millisecond, // Debounce
		func(path string, isConfig bool) {
			d.handleFileChange(ctx, svcName, svc, path, isConfig, noSync)
		},
	)
	if err != nil {
		return err
	}

	if err := watcher.Start(); err != nil {
		return err
	}

	d.mu.Lock()
	d.watchers = append(d.watchers, watcher)
	d.mu.Unlock()

	d.console.Debug("Watching %s: %v", svcName, absPaths)

	return nil
}

func (d *DevLoop) handleFileChange(ctx context.Context, svcName string, svc config.Service, path string, isConfig, noSync bool) {
	relPath, err := filepath.Rel(".", path)
	if err != nil {
		relPath = path
	}

	if isConfig {
		// Try hot-patching first (much faster than rebuild)
		containerName := fmt.Sprintf("%s_%s", d.config.Project.Name, svcName)
		if d.runtime.IsContainerRunning(context.Background(), containerName) {
			d.console.ServiceLog(svcName, fmt.Sprintf("Config changed: %s - hot-patching...", relPath))
			if err := d.hotPatchDeps(svcName, svc, containerName); err != nil {
				d.console.ServiceLog(svcName, fmt.Sprintf("Hot-patch failed: %s - falling back to rebuild", err))
				d.rebuildAndRestart(ctx, svcName, svc)
			}
			return
		}
		// Container not running, do full rebuild
		d.console.ServiceLog(svcName, fmt.Sprintf("Config changed: %s - rebuilding...", relPath))
		d.rebuildAndRestart(ctx, svcName, svc)
	} else if noSync {
		// No sync mode - rebuild on any change
		d.console.ServiceLog(svcName, fmt.Sprintf("Changed: %s - rebuilding...", relPath))
		d.rebuildAndRestart(ctx, svcName, svc)
	} else {
		// Sync mode - bind mount handles it, just log
		d.console.ServiceLog(svcName, fmt.Sprintf("Changed: %s", relPath))
		// The file change is already visible in the container via bind mount
		// The app's HMR (nodemon, vite, etc.) will handle the reload
	}
}

func (d *DevLoop) rebuildAndRestart(ctx context.Context, svcName string, svc config.Service) {
	containerName := fmt.Sprintf("%s_%s", d.config.Project.Name, svcName)
	networkName := fmt.Sprintf("cbox_%s", d.config.Project.Name)

	// Stop old container
	d.runtime.StopContainer(ctx, containerName, 5*time.Second)
	d.runtime.RemoveContainer(ctx, containerName)

	// Rebuild
	imageName := fmt.Sprintf("%s-%s:latest", d.config.Project.Name, svcName)
	_, err := d.builder.Build(ctx, builder.BuildOptions{
		ServiceName: svcName,
		Service:     svc,
		ProjectName: d.config.Project.Name,
		DevMode:     true,
		Tag:         imageName,
	})

	if err != nil {
		d.console.Error("Failed to rebuild %s: %s", svcName, err)
		return
	}

	// Recreate and start container
	containerCfg := runtime.ContainerConfigFromService(
		svcName,
		svc,
		d.config.Project.Name,
		networkName,
		imageName,
		true, // dev mode
		"",   // no namespace in dev mode
	)

	_, err = d.runtime.CreateContainer(ctx, containerCfg)
	if err != nil {
		d.console.Error("Failed to create container %s: %s", svcName, err)
		return
	}

	err = d.runtime.StartContainer(ctx, containerName)
	if err != nil {
		d.console.Error("Failed to start container %s: %s", svcName, err)
		return
	}

	d.console.Success("Restarted %s", svcName)
}

// hotPatchDeps installs dependencies inside a running container without rebuilding.
func (d *DevLoop) hotPatchDeps(svcName string, svc config.Service, containerName string) error {
	// Determine the install command based on runtime
	installCmd := d.getInstallCommand(svc)
	if installCmd == "" {
		return fmt.Errorf("unknown runtime for hot-patching: %s", svc.Runtime)
	}

	// Pause watchers to prevent infinite loop from lock file changes
	d.pauseWatchers()

	// Run install command inside container
	start := time.Now()
	output, err := d.runtime.ContainerExecWithOutput(
		context.Background(),
		containerName,
		installCmd,
	)
	if err != nil {
		d.resumeWatchers()
		return fmt.Errorf("install failed: %w (output: %s)", err, output)
	}

	elapsed := time.Since(start)
	d.console.ServiceLog(svcName, fmt.Sprintf("Dependencies updated in %.1fs", elapsed.Seconds()))

	// Wait for bind mount to sync lock files back, then resume watcher
	go func() {
		time.Sleep(500 * time.Millisecond)
		d.resumeWatchers()
	}()

	// Background rebuild to keep image in sync (cancellable)
	d.triggerBackgroundRebuild(svcName, svc)

	return nil
}

// getInstallCommand returns the package manager install command for a runtime.
func (d *DevLoop) getInstallCommand(svc config.Service) string {
	switch svc.Runtime {
	case "nodejs", "node":
		// Check for lock files to determine package manager
		// Default to npm
		return "npm install"
	case "go", "golang":
		return "go mod tidy && go mod download"
	case "python":
		return "pip install -r requirements.txt"
	default:
		return ""
	}
}

// triggerBackgroundRebuild silently rebuilds the image to keep it in sync.
func (d *DevLoop) triggerBackgroundRebuild(svcName string, svc config.Service) {
	// Cancel any in-flight background build
	if d.buildCancel != nil {
		d.buildCancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.buildCancel = cancel
	d.buildCtx = ctx

	go func() {
		imageName := fmt.Sprintf("%s-%s:latest", d.config.Project.Name, svcName)
		_, err := d.builder.Build(ctx, builder.BuildOptions{
			ServiceName: svcName,
			Service:     svc,
			ProjectName: d.config.Project.Name,
			DevMode:     true,
			Tag:         imageName,
		})
		if err != nil && ctx.Err() == nil {
			d.console.Debug("Background rebuild failed for %s: %s", svcName, err)
		}
	}()
}

// pauseWatchers pauses all file watchers.
func (d *DevLoop) pauseWatchers() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, w := range d.watchers {
		w.Pause()
	}
}

// resumeWatchers resumes all file watchers.
func (d *DevLoop) resumeWatchers() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, w := range d.watchers {
		w.Resume()
	}
}

func (d *DevLoop) printDevInfo(services []string) {
	d.console.Newline()

	lines := []string{
		"cbox dev mode",
		"",
	}

	for _, svcName := range services {
		svc := d.config.Services[svcName]
		port := svc.GetPrimaryPort()
		if port > 0 {
			url := fmt.Sprintf("http://localhost:%d", port)
			lines = append(lines, fmt.Sprintf("%-10s %s", svcName, url))
		} else {
			lines = append(lines, svcName)
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Watching for changes...")
	lines = append(lines, "Press Ctrl+C to stop")

	d.console.Box(lines)
	d.console.Newline()
}

func (d *DevLoop) streamAllLogs(ctx context.Context, services []string) {
	var wg sync.WaitGroup

	for _, svcName := range services {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			containerName := fmt.Sprintf("%s_%s", d.config.Project.Name, name)

			// Wait a bit for container to start
			time.Sleep(2 * time.Second)

			reader, err := d.runtime.ContainerLogs(ctx, containerName, true, 10)
			if err != nil {
				d.console.Warn("Could not stream logs for %s: %s", name, err)
				return
			}
			defer reader.Close()

			scanner := bufio.NewScanner(reader)
			lastPanelRedraw := time.Time{}
			for scanner.Scan() {
				select {
				case <-ctx.Done():
					return
				default:
					d.console.ServiceLog(name, scanner.Text())

					// Redraw panel after log output (rate-limited to avoid flicker)
					if d.panel != nil && d.panel.IsEnabled() && time.Since(lastPanelRedraw) > 200*time.Millisecond {
						d.panel.Redraw()
						lastPanelRedraw = time.Now()
					}
				}
			}
		}(svcName)
	}

	wg.Wait()
}

// notifyPanel sends a status message to the intelligence panel.
func (d *DevLoop) notifyPanel(msg string) {
	if d.panel != nil {
		d.panel.SetAction(msg)
		d.panel.Redraw()
	}
}

// pollServiceStats periodically fetches container stats and updates the panel.
func (d *DevLoop) pollServiceStats(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			labels := map[string]string{"cbox.project": d.config.Project.Name}
			stats, err := d.runtime.GetContainerStats(ctx, labels)
			if err != nil {
				continue
			}
			for name, stat := range stats {
				svcName := strings.TrimPrefix(name, d.config.Project.Name+"_")
				port := 0
				if svc, ok := d.config.Services[svcName]; ok {
					port = svc.GetPrimaryPort()
				}
				d.panel.UpdateService(svcName, "Running", stat.Memory, port)
			}
			d.panel.Redraw()
		}
	}
}

func (d *DevLoop) stopWatchers() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, w := range d.watchers {
		w.Stop()
	}
	d.watchers = nil
}
