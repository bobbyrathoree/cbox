package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "cbox.yaml")

	content := `version: "1"
services:
  app:
    path: .
    port: 3000
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("Load() Version = %q, want %q", cfg.Version, "1")
	}
	if _, exists := cfg.Services["app"]; !exists {
		t.Error("Load() missing service 'app'")
	}
	if cfg.Services["app"].Port != 3000 {
		t.Errorf("Load() app.Port = %d, want %d", cfg.Services["app"].Port, 3000)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/tmp/nonexistent-cbox-config-12345.yaml")
	if err == nil {
		t.Fatal("Load() expected error for missing file, got nil")
	}
	if !contains(err.Error(), "not found") {
		t.Errorf("Load() error = %q, want error containing 'not found'", err.Error())
	}
}

func TestLoad_NonRegularFile(t *testing.T) {
	_, err := Load("/dev/null")
	if err == nil {
		t.Fatal("Load() expected error for non-regular file, got nil")
	}
	if !contains(err.Error(), "not a regular file") {
		t.Errorf("Load() error = %q, want error containing 'not a regular file'", err.Error())
	}
}

func TestParse_ValidConfig(t *testing.T) {
	yaml := []byte(`version: "1"
services:
  api:
    path: ./api
    port: 8080
    runtime: nodejs
`)
	cfg, err := Parse(yaml, "/tmp")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("Parse() Version = %q, want %q", cfg.Version, "1")
	}
	svc, exists := cfg.Services["api"]
	if !exists {
		t.Fatal("Parse() missing service 'api'")
	}
	if svc.Path != "./api" {
		t.Errorf("Parse() api.Path = %q, want %q", svc.Path, "./api")
	}
	if svc.Port != 8080 {
		t.Errorf("Parse() api.Port = %d, want %d", svc.Port, 8080)
	}
	if svc.Runtime != "nodejs" {
		t.Errorf("Parse() api.Runtime = %q, want %q", svc.Runtime, "nodejs")
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte("{{invalid"), "/tmp")
	if err == nil {
		t.Fatal("Parse() expected error for invalid YAML, got nil")
	}
}

func TestParse_EnvVarSubstitution(t *testing.T) {
	t.Run("substitutes set variable", func(t *testing.T) {
		os.Setenv("CBOX_TEST_VAR", "hello")
		defer os.Unsetenv("CBOX_TEST_VAR")

		yaml := []byte(`version: "1"
services:
  app:
    path: .
    port: 3000
    env:
      MY_VAR: ${CBOX_TEST_VAR}
`)
		cfg, err := Parse(yaml, "/tmp")
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}

		if cfg.Services["app"].Env["MY_VAR"] != "hello" {
			t.Errorf("Parse() MY_VAR = %q, want %q", cfg.Services["app"].Env["MY_VAR"], "hello")
		}
	})

	t.Run("uses default for missing variable", func(t *testing.T) {
		// Make sure the variable is not set
		os.Unsetenv("CBOX_MISSING_VAR_FOR_TEST")

		yaml := []byte(`version: "1"
services:
  app:
    path: .
    port: 3000
    env:
      MY_VAR: ${CBOX_MISSING_VAR_FOR_TEST:-default_value}
`)
		cfg, err := Parse(yaml, "/tmp")
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}

		if cfg.Services["app"].Env["MY_VAR"] != "default_value" {
			t.Errorf("Parse() MY_VAR = %q, want %q", cfg.Services["app"].Env["MY_VAR"], "default_value")
		}
	})
}

func TestParse_CircularDeps(t *testing.T) {
	yaml := []byte(`version: "1"
services:
  a:
    image: nginx
    depends_on: [b]
  b:
    image: nginx
    depends_on: [a]
`)
	_, err := Parse(yaml, "/tmp")
	if err == nil {
		t.Fatal("Parse() expected error for circular dependency, got nil")
	}
	if !contains(err.Error(), "circular") {
		t.Errorf("Parse() error = %q, want error containing 'circular'", err.Error())
	}
}

func TestParse_DefaultsApplied(t *testing.T) {
	yaml := []byte(`version: "1"
services:
  app:
    path: .
    port: 3000
`)
	cfg, err := Parse(yaml, "/tmp")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	svc := cfg.Services["app"]

	if svc.Healthcheck.Interval != 5*time.Second {
		t.Errorf("Parse() default Healthcheck.Interval = %v, want %v", svc.Healthcheck.Interval, 5*time.Second)
	}
	if svc.Healthcheck.Timeout != 3*time.Second {
		t.Errorf("Parse() default Healthcheck.Timeout = %v, want %v", svc.Healthcheck.Timeout, 3*time.Second)
	}
	if svc.Healthcheck.Retries != 3 {
		t.Errorf("Parse() default Healthcheck.Retries = %d, want %d", svc.Healthcheck.Retries, 3)
	}
	if svc.Healthcheck.StartPeriod != 10*time.Second {
		t.Errorf("Parse() default Healthcheck.StartPeriod = %v, want %v", svc.Healthcheck.StartPeriod, 10*time.Second)
	}
}

// contains checks if substr is contained within s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
