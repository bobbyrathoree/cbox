package tunnel

import (
	"testing"
)

func TestParseHost(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedUser string
		expectedHost string
		expectedPort int
	}{
		{
			name:         "full format",
			input:        "user@example.com:2222",
			expectedUser: "user",
			expectedHost: "example.com",
			expectedPort: 2222,
		},
		{
			name:         "user and host only",
			input:        "user@example.com",
			expectedUser: "user",
			expectedHost: "example.com",
			expectedPort: 22,
		},
		{
			name:         "host only",
			input:        "example.com",
			expectedUser: "", // Will be set to current user, tested separately
			expectedHost: "example.com",
			expectedPort: 22,
		},
		{
			name:         "host and port",
			input:        "example.com:3022",
			expectedUser: "",
			expectedHost: "example.com",
			expectedPort: 3022,
		},
		{
			name:         "ip address",
			input:        "admin@192.168.1.1:22",
			expectedUser: "admin",
			expectedHost: "192.168.1.1",
			expectedPort: 22,
		},
		{
			name:         "ip with custom port",
			input:        "root@10.0.0.1:2222",
			expectedUser: "root",
			expectedHost: "10.0.0.1",
			expectedPort: 2222,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, host, port := ParseHost(tt.input)

			// For tests without explicit user, we allow any user (including current user)
			if tt.expectedUser != "" && user != tt.expectedUser {
				t.Errorf("ParseHost(%q) user = %q, want %q", tt.input, user, tt.expectedUser)
			}
			if host != tt.expectedHost {
				t.Errorf("ParseHost(%q) host = %q, want %q", tt.input, host, tt.expectedHost)
			}
			if port != tt.expectedPort {
				t.Errorf("ParseHost(%q) port = %d, want %d", tt.input, port, tt.expectedPort)
			}
		})
	}
}

func TestParseHost_DefaultUser(t *testing.T) {
	// When no user is specified, it should default to current user
	user, host, port := ParseHost("example.com")

	if host != "example.com" {
		t.Errorf("host = %q, want example.com", host)
	}
	if port != 22 {
		t.Errorf("port = %d, want 22", port)
	}
	// User should be set to something (current user)
	// We don't check the exact value as it depends on the system
	t.Logf("Default user: %q", user)
}

func TestPortMapping(t *testing.T) {
	pm := PortMapping{
		LocalPort:  3000,
		RemotePort: 8080,
	}

	if pm.LocalPort != 3000 {
		t.Errorf("LocalPort = %d, want 3000", pm.LocalPort)
	}
	if pm.RemotePort != 8080 {
		t.Errorf("RemotePort = %d, want 8080", pm.RemotePort)
	}
}

func TestConfig(t *testing.T) {
	cfg := Config{
		Host: "example.com",
		User: "admin",
		Port: 22,
		Mappings: []PortMapping{
			{LocalPort: 3000, RemotePort: 80},
			{LocalPort: 5432, RemotePort: 5432},
		},
	}

	if cfg.Host != "example.com" {
		t.Errorf("Host = %q, want example.com", cfg.Host)
	}
	if cfg.User != "admin" {
		t.Errorf("User = %q, want admin", cfg.User)
	}
	if cfg.Port != 22 {
		t.Errorf("Port = %d, want 22", cfg.Port)
	}
	if len(cfg.Mappings) != 2 {
		t.Errorf("len(Mappings) = %d, want 2", len(cfg.Mappings))
	}
}

// MockLogger implements Logger interface for testing
type MockLogger struct {
	InfoCalls    []string
	SuccessCalls []string
	ErrorCalls   []string
	WarnCalls    []string
}

func (m *MockLogger) Info(format string, args ...interface{})    { m.InfoCalls = append(m.InfoCalls, format) }
func (m *MockLogger) Success(format string, args ...interface{}) { m.SuccessCalls = append(m.SuccessCalls, format) }
func (m *MockLogger) Error(format string, args ...interface{})   { m.ErrorCalls = append(m.ErrorCalls, format) }
func (m *MockLogger) Warn(format string, args ...interface{})    { m.WarnCalls = append(m.WarnCalls, format) }

func TestNew_NoSSHKeys(t *testing.T) {
	// This test may fail on systems without SSH keys, which is expected
	// We're mainly testing that New doesn't panic
	logger := &MockLogger{}

	cfg := Config{
		Host: "localhost",
		User: "testuser",
		Port: 22,
	}

	_, err := New(cfg, logger)
	// Error is expected if no SSH keys are available
	if err != nil {
		t.Logf("New() returned error (expected on systems without SSH keys): %v", err)
	}
}

func TestTunnel_Close_NilClient(t *testing.T) {
	// Test that Close doesn't panic with nil client
	tun := &Tunnel{
		client: nil,
	}

	err := tun.Close()
	if err != nil {
		t.Errorf("Close() with nil client should not error: %v", err)
	}
}
