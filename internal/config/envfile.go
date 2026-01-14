package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ParseEnvFile reads a .env file and returns a map of key-value pairs.
// Supports:
// - KEY=value
// - KEY="quoted value"
// - KEY='single quoted value'
// - # comments
// - Empty lines (ignored)
// - Export prefix: export KEY=value
func ParseEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open env file: %w", err)
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix
		line = strings.TrimPrefix(line, "export ")
		line = strings.TrimSpace(line)

		// Find the first = sign
		idx := strings.Index(line, "=")
		if idx == -1 {
			return nil, fmt.Errorf("line %d: invalid format (expected KEY=value)", lineNum)
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Validate key
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNum)
		}

		// Remove surrounding quotes from value
		value = unquote(value)

		result[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read env file: %w", err)
	}

	return result, nil
}

// unquote removes surrounding double or single quotes from a string.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}

	// Handle double quotes
	if s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}

	// Handle single quotes
	if s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}

	return s
}
