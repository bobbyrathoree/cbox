package config

import (
	"os"
	"path/filepath"
)

// DefaultConfig creates a minimal default configuration for zero-config mode.
// This is used when cbox.yaml doesn't exist but we detect a project.
func DefaultConfig(projectPath string) *Config {
	projectName := filepath.Base(projectPath)
	if projectName == "." {
		if wd, err := os.Getwd(); err == nil {
			projectName = filepath.Base(wd)
		}
	}

	return &Config{
		Version: "1",
		Project: ProjectConfig{
			Name: projectName,
		},
		Services: make(map[string]Service),
		Volumes:  make(map[string]Volume),
		Secrets:  make(map[string]Secret),
	}
}

// DefaultNodeService creates default configuration for a Node.js service.
func DefaultNodeService(name, path string, port int) Service {
	return Service{
		Path:    path,
		Runtime: "nodejs",
		Port:    port,
		Command: []string{"npm", "start"},
		Dev: DevConfig{
			Command: []string{"npm", "run", "dev"},
			Sync:    true,
			Watch: WatchConfig{
				Paths:  []string{"src/", "package.json"},
				Ignore: []string{"node_modules/", "dist/", ".next/"},
			},
		},
		Env: map[string]string{
			"NODE_ENV": "development",
		},
	}
}

// DefaultPostgresService creates default configuration for PostgreSQL.
func DefaultPostgresService(name string, port int) Service {
	return Service{
		Image: "postgres:16-alpine",
		Port:  port,
		Env: map[string]string{
			"POSTGRES_DB":       "app",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres", // Should use secrets in production
		},
		Volumes: []string{
			name + "_data:/var/lib/postgresql/data",
		},
	}
}

// DefaultRedisService creates default configuration for Redis.
func DefaultRedisService(name string, port int) Service {
	return Service{
		Image: "redis:7-alpine",
		Port:  port,
	}
}

// RuntimeDefaults contains default settings for each supported runtime.
var RuntimeDefaults = map[string]struct {
	BaseImage  string
	DefaultPort int
	DevCommand []string
}{
	"nodejs": {
		BaseImage:   "node:20-slim",
		DefaultPort: 3000,
		DevCommand:  []string{"npm", "run", "dev"},
	},
	"node": {
		BaseImage:   "node:20-slim",
		DefaultPort: 3000,
		DevCommand:  []string{"npm", "run", "dev"},
	},
	"go": {
		BaseImage:   "golang:1.22-alpine",
		DefaultPort: 8080,
		DevCommand:  []string{"go", "run", "."},
	},
	"golang": {
		BaseImage:   "golang:1.22-alpine",
		DefaultPort: 8080,
		DevCommand:  []string{"go", "run", "."},
	},
	"python": {
		BaseImage:   "python:3.12-slim",
		DefaultPort: 8000,
		DevCommand:  []string{"python", "-m", "uvicorn", "main:app", "--reload"},
	},
}

// GetRuntimeDefault returns default settings for a runtime, or nil if unknown.
func GetRuntimeDefault(runtime string) *struct {
	BaseImage   string
	DefaultPort int
	DevCommand  []string
} {
	if defaults, ok := RuntimeDefaults[runtime]; ok {
		return &defaults
	}
	return nil
}
