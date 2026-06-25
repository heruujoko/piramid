package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultUsesExpectedServerAndWorkers(t *testing.T) {
	cfg := Default()

	if cfg.Version != 1 {
		t.Fatalf("Version = %d, want 1", cfg.Version)
	}
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 7433 {
		t.Fatalf("Server = %#v", cfg.Server)
	}
	if cfg.Workers.Count != 3 {
		t.Fatalf("Workers.Count = %d, want 3", cfg.Workers.Count)
	}
}

func TestValidateRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Config)
		contains string
	}{
		{"empty host", func(c *Config) { c.Server.Host = "" }, "server.host"},
		{"low port", func(c *Config) { c.Server.Port = 0 }, "server.port"},
		{"high port", func(c *Config) { c.Server.Port = 65536 }, "server.port"},
		{"worker count", func(c *Config) { c.Workers.Count = 0 }, "workers.count"},
		{"unknown adapter", func(c *Config) { c.Runtime.Executor.Adapter = "other" }, "runtime.executor.adapter"},
		{"empty command", func(c *Config) { c.Runtime.Executor.Command = "" }, "runtime.executor.command"},
		{"unknown placeholder", func(c *Config) {
			c.Runtime.Executor.Args = []string{"{{unknown}}"}
		}, "runtime.executor.args"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.contains)
			}
		})
	}
}

func TestLoadRejectsUnknownYAMLFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nmystery: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "field mystery not found") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadParsesRuntimeTimeouts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `version: 1
server:
  host: 127.0.0.1
  port: 7433
workers:
  count: 2
runtime:
  planner:
    adapter: pi-cli
    command: pi
    args: ["-p", "{{prompt}}"]
    timeout: 30m
  executor:
    adapter: command
    command: fixture
    args: ["{{prompt_file}}", "{{workspace}}"]
    timeout: 4h
  verifier:
    adapter: pi-cli
    command: pi
    args: ["-p", "{{prompt}}"]
    timeout: 1h
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.Executor.Timeout.String() != "4h0m0s" {
		t.Fatalf("executor timeout = %s", cfg.Runtime.Executor.Timeout)
	}
}
