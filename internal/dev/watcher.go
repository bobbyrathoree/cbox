// Package dev handles development mode with file watching.
package dev

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches files for changes and triggers callbacks.
type Watcher struct {
	watcher  *fsnotify.Watcher
	paths    []string
	ignore   []string
	debounce time.Duration
	onChange func(path string, isConfig bool)
	mu       sync.Mutex
	lastFire time.Time
}

// NewWatcher creates a new file watcher.
func NewWatcher(paths, ignore []string, debounce time.Duration, onChange func(path string, isConfig bool)) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		watcher:  fw,
		paths:    paths,
		ignore:   ignore,
		debounce: debounce,
		onChange: onChange,
	}, nil
}

// Start begins watching for file changes.
func (w *Watcher) Start() error {
	// Add paths to watch
	for _, path := range w.paths {
		if err := w.addPath(path); err != nil {
			return err
		}
	}

	// Watch for events
	go w.loop()

	return nil
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	w.watcher.Close()
}

func (w *Watcher) addPath(path string) error {
	// Walk directory and add all subdirectories
	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip ignored patterns
		if w.shouldIgnore(p) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Add directories to watcher
		if d.IsDir() {
			w.watcher.Add(p)
		}

		return nil
	})
}

func (w *Watcher) shouldIgnore(path string) bool {
	name := filepath.Base(path)

	// Always ignore common directories
	alwaysIgnore := []string{
		"node_modules",
		".git",
		".cbox",
		"__pycache__",
		".next",
		"dist",
		"build",
		".venv",
		"venv",
		"vendor",
	}

	for _, ignore := range alwaysIgnore {
		if name == ignore {
			return true
		}
	}

	// Check custom ignore patterns
	for _, pattern := range w.ignore {
		// Simple pattern matching
		if strings.HasSuffix(pattern, "/") {
			// Directory pattern
			if name == strings.TrimSuffix(pattern, "/") {
				return true
			}
		} else if strings.HasPrefix(pattern, "*.") {
			// Extension pattern
			ext := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(path, ext) {
				return true
			}
		} else if name == pattern {
			return true
		}
	}

	return false
}

func (w *Watcher) loop() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Only care about write and create events
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			// Skip ignored files
			if w.shouldIgnore(event.Name) {
				continue
			}

			// Debounce
			w.mu.Lock()
			if time.Since(w.lastFire) < w.debounce {
				w.mu.Unlock()
				continue
			}
			w.lastFire = time.Now()
			w.mu.Unlock()

			// Determine if this is a config file change
			isConfig := isConfigFile(event.Name)

			// Trigger callback
			w.onChange(event.Name, isConfig)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			_ = err // Log errors if needed
		}
	}
}

// isConfigFile returns true if the file is a configuration or dependency file.
func isConfigFile(path string) bool {
	name := filepath.Base(path)
	configFiles := []string{
		"package.json",
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"bun.lockb",
		"go.mod",
		"go.sum",
		"requirements.txt",
		"pyproject.toml",
		"Pipfile",
		"Pipfile.lock",
		"cbox.yaml",
		"Dockerfile",
		".dockerignore",
	}

	for _, cf := range configFiles {
		if name == cf {
			return true
		}
	}

	return false
}
