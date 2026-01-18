package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR} or ${VAR:-default} patterns.
var envVarPattern = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

var validate = validator.New()

// Load reads and parses a cbox.yaml file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return Parse(data, filepath.Dir(path))
}

// Parse parses cbox.yaml content and applies defaults.
func Parse(data []byte, baseDir string) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	// Substitute environment variables (${VAR} syntax)
	// Skip the Environments section - those are substituted lazily when WithEnvironment() is called
	if err := substituteEnvVarsExcludeEnvironments(&cfg); err != nil {
		return nil, fmt.Errorf("env substitution failed: %w", err)
	}

	// Load .env files for services
	if err := loadEnvFiles(&cfg, baseDir); err != nil {
		return nil, fmt.Errorf("env file loading failed: %w", err)
	}

	// Resolve secrets
	if err := resolveSecrets(&cfg, baseDir); err != nil {
		return nil, fmt.Errorf("secret resolution failed: %w", err)
	}

	// Apply defaults
	applyDefaults(&cfg, baseDir)

	// Validate
	if err := validate.Struct(cfg); err != nil {
		return nil, formatValidationErrors(err)
	}

	// Additional validation
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyDefaults fills in default values for the configuration.
func applyDefaults(cfg *Config, baseDir string) {
	// Default project name to directory name
	if cfg.Project.Name == "" {
		cfg.Project.Name = filepath.Base(baseDir)
		if cfg.Project.Name == "." || cfg.Project.Name == "/" {
			cfg.Project.Name = "cbox-project"
		}
	}

	// Apply service defaults
	for name, svc := range cfg.Services {
		applyServiceDefaults(&svc, name)
		cfg.Services[name] = svc
	}
}

// applyServiceDefaults fills in default values for a service.
func applyServiceDefaults(svc *Service, name string) {
	// Default healthcheck values
	if svc.Healthcheck.Interval == 0 {
		svc.Healthcheck.Interval = 5 * time.Second
	}
	if svc.Healthcheck.Timeout == 0 {
		svc.Healthcheck.Timeout = 3 * time.Second
	}
	if svc.Healthcheck.Retries == 0 {
		svc.Healthcheck.Retries = 3
	}
	if svc.Healthcheck.StartPeriod == 0 {
		svc.Healthcheck.StartPeriod = 10 * time.Second
	}

	// Default dev sync to true for build services
	if svc.IsBuildService() && !svc.Dev.Sync {
		svc.Dev.Sync = true
	}

	// Default watch paths based on runtime
	if len(svc.Dev.Watch.Paths) == 0 && svc.IsBuildService() {
		switch svc.Runtime {
		case "nodejs", "node":
			svc.Dev.Watch.Paths = []string{"src/", "package.json"}
			svc.Dev.Watch.Ignore = []string{"node_modules/", "*.test.ts", "*.test.js", "dist/", ".next/"}
		case "go", "golang":
			svc.Dev.Watch.Paths = []string{"."}
			svc.Dev.Watch.Ignore = []string{"vendor/", "*_test.go"}
		case "python":
			svc.Dev.Watch.Paths = []string{"."}
			svc.Dev.Watch.Ignore = []string{"__pycache__/", "*.pyc", ".venv/", "venv/"}
		}
	}
}

// validateConfig performs additional validation beyond struct tags.
func validateConfig(cfg *Config) error {
	for name, svc := range cfg.Services {
		// Each service must have either path or image
		if svc.Path == "" && svc.Image == "" {
			return fmt.Errorf("service %q: must specify either 'path' (build) or 'image'", name)
		}
		if svc.Path != "" && svc.Image != "" {
			return fmt.Errorf("service %q: cannot specify both 'path' and 'image'", name)
		}

		// Validate port number
		if svc.Port != 0 && (svc.Port < 1 || svc.Port > 65535) {
			return fmt.Errorf("service %q: invalid port %d (must be 1-65535)", name, svc.Port)
		}

		// Validate runtime if specified
		if svc.Runtime != "" && !isValidRuntime(svc.Runtime) {
			return fmt.Errorf("service %q: unknown runtime %q (supported: nodejs, python, go)", name, svc.Runtime)
		}

		// Validate dependencies exist
		for _, dep := range svc.DependsOn {
			if _, exists := cfg.Services[dep]; !exists {
				return fmt.Errorf("service %q: depends on unknown service %q", name, dep)
			}
		}

		// Validate secret references
		for _, secretRef := range svc.Secrets {
			if _, exists := cfg.Secrets[secretRef]; !exists {
				return fmt.Errorf("service %q: references unknown secret %q", name, secretRef)
			}
		}

		// Validate volume references
		for _, vol := range svc.Volumes {
			// Skip bind mounts (start with ./ or /)
			if vol[0] == '.' || vol[0] == '/' {
				continue
			}
			// Extract volume name (before the colon)
			volName := vol
			if idx := findColon(vol); idx != -1 {
				volName = vol[:idx]
			}
			if _, exists := cfg.Volumes[volName]; !exists {
				return fmt.Errorf("service %q: references unknown volume %q", name, volName)
			}
		}
	}

	// Check for circular dependencies
	if err := checkCircularDeps(cfg); err != nil {
		return err
	}

	return nil
}

// checkCircularDeps detects circular dependencies between services.
func checkCircularDeps(cfg *Config) error {
	// Use DFS to detect cycles
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(name string) error
	dfs = func(name string) error {
		visited[name] = true
		recStack[name] = true

		svc := cfg.Services[name]
		for _, dep := range svc.DependsOn {
			if !visited[dep] {
				if err := dfs(dep); err != nil {
					return err
				}
			} else if recStack[dep] {
				return fmt.Errorf("circular dependency detected: %s -> %s", name, dep)
			}
		}

		recStack[name] = false
		return nil
	}

	for name := range cfg.Services {
		if !visited[name] {
			if err := dfs(name); err != nil {
				return err
			}
		}
	}

	return nil
}

// findColon returns the index of the first colon, or -1 if not found.
func findColon(s string) int {
	for i, c := range s {
		if c == ':' {
			return i
		}
	}
	return -1
}

// formatValidationErrors converts validator errors to a readable message.
func formatValidationErrors(err error) error {
	if validationErrs, ok := err.(validator.ValidationErrors); ok {
		var msg string
		for _, e := range validationErrs {
			field := e.Field()
			tag := e.Tag()
			switch tag {
			case "required":
				msg += fmt.Sprintf("  - %s is required\n", field)
			case "min":
				msg += fmt.Sprintf("  - %s must have at least %s items\n", field, e.Param())
			case "eq":
				msg += fmt.Sprintf("  - %s must be '%s'\n", field, e.Param())
			default:
				msg += fmt.Sprintf("  - %s failed validation: %s\n", field, tag)
			}
		}
		return fmt.Errorf("configuration validation failed:\n%s", msg)
	}
	return err
}

// Exists checks if a config file exists at the given path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// substituteEnvVars replaces ${VAR} and ${VAR:-default} patterns in all string fields.
func substituteEnvVars(cfg *Config) error {
	return substituteEnvVarsInValue(reflect.ValueOf(cfg))
}

// substituteEnvVarsExcludeEnvironments substitutes env vars but skips the Environments section.
// This allows lazy evaluation of environment-specific variables when WithEnvironment() is called.
func substituteEnvVarsExcludeEnvironments(cfg *Config) error {
	// Temporarily store and nil out Environments
	savedEnvs := cfg.Environments
	cfg.Environments = nil

	// Substitute in everything except environments
	err := substituteEnvVarsInValue(reflect.ValueOf(cfg))

	// Restore environments (unsubstituted)
	cfg.Environments = savedEnvs
	return err
}

// substituteEnvVarsInValue recursively processes a reflect.Value for env substitution.
func substituteEnvVarsInValue(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Ptr:
		if !v.IsNil() {
			return substituteEnvVarsInValue(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := substituteEnvVarsInValue(v.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			// For map values, we need special handling
			if val.Kind() == reflect.String {
				newVal, err := ExpandEnvString(val.String())
				if err != nil {
					return err
				}
				v.SetMapIndex(key, reflect.ValueOf(newVal))
			} else if val.Kind() == reflect.Struct || val.Kind() == reflect.Ptr {
				// For struct values in maps, we need to work with addressable copies
				newVal := reflect.New(val.Type()).Elem()
				newVal.Set(val)
				if err := substituteEnvVarsInValue(newVal); err != nil {
					return err
				}
				v.SetMapIndex(key, newVal)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i)
			if elem.Kind() == reflect.String && elem.CanSet() {
				newVal, err := ExpandEnvString(elem.String())
				if err != nil {
					return err
				}
				elem.SetString(newVal)
			} else {
				if err := substituteEnvVarsInValue(elem); err != nil {
					return err
				}
			}
		}
	case reflect.String:
		if v.CanSet() {
			newVal, err := ExpandEnvString(v.String())
			if err != nil {
				return err
			}
			v.SetString(newVal)
		}
	}
	return nil
}

// ExpandEnvString replaces ${VAR} and ${VAR:-default} in a string.
func ExpandEnvString(s string) (string, error) {
	var lastErr error
	result := envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		submatches := envVarPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		varName := submatches[1]
		defaultVal := ""
		hasDefault := len(submatches) >= 3 && submatches[2] != ""
		if hasDefault {
			defaultVal = submatches[2]
		}

		value := os.Getenv(varName)
		if value == "" {
			if hasDefault {
				return defaultVal
			}
			// Check if the variable exists but is empty vs not set at all
			if _, exists := os.LookupEnv(varName); !exists {
				lastErr = fmt.Errorf("environment variable %q is not set and has no default", varName)
				return match
			}
		}
		return value
	})

	return result, lastErr
}

// loadEnvFiles loads .env files specified in service configs.
func loadEnvFiles(cfg *Config, baseDir string) error {
	for name, svc := range cfg.Services {
		if svc.EnvFile == "" {
			continue
		}

		envPath := svc.EnvFile
		if !filepath.IsAbs(envPath) {
			envPath = filepath.Join(baseDir, envPath)
		}

		envVars, err := ParseEnvFile(envPath)
		if err != nil {
			return fmt.Errorf("service %q: %w", name, err)
		}

		// Initialize Env map if nil
		if svc.Env == nil {
			svc.Env = make(map[string]string)
		}

		// Merge env file values (explicit env values take precedence)
		for k, v := range envVars {
			if _, exists := svc.Env[k]; !exists {
				svc.Env[k] = v
			}
		}

		cfg.Services[name] = svc
	}
	return nil
}

// resolveSecrets resolves secret values and injects them into service environments.
func resolveSecrets(cfg *Config, baseDir string) error {
	// First, resolve all secret values
	resolvedSecrets := make(map[string]string)

	for name, secret := range cfg.Secrets {
		var value string

		if secret.Env != "" {
			value = os.Getenv(secret.Env)
			if value == "" {
				if _, exists := os.LookupEnv(secret.Env); !exists {
					return fmt.Errorf("secret %q: environment variable %q is not set", name, secret.Env)
				}
			}
		} else if secret.File != "" {
			filePath := secret.File
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(baseDir, filePath)
			}

			data, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("secret %q: failed to read file %q: %w", name, secret.File, err)
			}
			value = strings.TrimSpace(string(data))
		}

		resolvedSecrets[name] = value
	}

	// Inject secrets into service environments
	for svcName, svc := range cfg.Services {
		if len(svc.Secrets) == 0 {
			continue
		}

		if svc.Env == nil {
			svc.Env = make(map[string]string)
		}

		for _, secretRef := range svc.Secrets {
			value, exists := resolvedSecrets[secretRef]
			if !exists {
				// This should be caught by validation, but double-check
				return fmt.Errorf("service %q: secret %q not found", svcName, secretRef)
			}

			// Inject secret as uppercase env var (SECRET_NAME -> SECRET_NAME)
			envKey := strings.ToUpper(secretRef)
			// Don't override explicit env values
			if _, exists := svc.Env[envKey]; !exists {
				svc.Env[envKey] = value
			}
		}

		cfg.Services[svcName] = svc
	}

	return nil
}

// validRuntimes lists all supported runtime values.
var validRuntimes = map[string]bool{
	"nodejs": true,
	"node":   true,
	"python": true,
	"go":     true,
	"golang": true,
}

// isValidRuntime checks if the given runtime is supported.
func isValidRuntime(runtime string) bool {
	return validRuntimes[strings.ToLower(runtime)]
}
