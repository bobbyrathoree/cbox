// Package context manages project context like active environment.
package context

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	contextDir  = ".cbox"
	envFile     = "env"
)

// GetCurrentEnv returns the currently active environment.
// Returns empty string if no environment is set.
func GetCurrentEnv() string {
	path := getEnvFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SetCurrentEnv sets the active environment.
func SetCurrentEnv(env string) error {
	dir := getContextDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := getEnvFilePath()
	return os.WriteFile(path, []byte(env+"\n"), 0644)
}

// ClearCurrentEnv removes the active environment setting.
func ClearCurrentEnv() error {
	path := getEnvFilePath()
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Not an error if file doesn't exist
	}
	return err
}

// getContextDir returns the path to the .cbox directory.
func getContextDir() string {
	return contextDir
}

// getEnvFilePath returns the path to the env file.
func getEnvFilePath() string {
	return filepath.Join(contextDir, envFile)
}

// ContextDirExists returns true if the .cbox directory exists.
func ContextDirExists() bool {
	_, err := os.Stat(contextDir)
	return err == nil
}

// InitContextDir creates the .cbox directory if it doesn't exist.
func InitContextDir() error {
	return os.MkdirAll(contextDir, 0755)
}
