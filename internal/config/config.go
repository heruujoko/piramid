package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return err
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	*d = Duration(value)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

type Config struct {
	Version int            `yaml:"version"`
	Server  ServerConfig   `yaml:"server"`
	Workers WorkersConfig  `yaml:"workers"`
	Runtime RuntimeConfigs `yaml:"runtime"`
	Retry   RetryConfig    `yaml:"retry"`
	Loops   LoopsConfig    `yaml:"loops"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type WorkersConfig struct {
	Count int `yaml:"count"`
}

type RuntimeConfigs struct {
	Planner  RuntimeConfig `yaml:"planner"`
	Executor RuntimeConfig `yaml:"executor"`
	Verifier RuntimeConfig `yaml:"verifier"`
}

type RuntimeConfig struct {
	Adapter string   `yaml:"adapter"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Timeout Duration `yaml:"timeout"`
}

type LoopsConfig struct {
	DefinitionRoot string `yaml:"definition_root"`
}

type RetryConfig struct {
	DefaultMaxAttempts int      `yaml:"default_max_attempts"`
	InitialDelay       Duration `yaml:"initial_delay"`
	MaxDelay           Duration `yaml:"max_delay"`
	Backoff            string   `yaml:"backoff"`
}

func Default() Config {
	pi := func(timeout time.Duration) RuntimeConfig {
		return RuntimeConfig{
			Adapter: "pi-cli",
			Command: "pi",
			Args:    []string{"-p", "{{prompt}}"},
			Timeout: Duration(timeout),
		}
	}
	return Config{
		Version: 1,
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 7433,
		},
		Workers: WorkersConfig{Count: 3},
		Runtime: RuntimeConfigs{
			Planner:  pi(30 * time.Minute),
			Executor: pi(4 * time.Hour),
			Verifier: pi(time.Hour),
		},
		Retry: RetryConfig{
			DefaultMaxAttempts: 3,
			InitialDelay:       Duration(time.Minute),
			MaxDelay:           Duration(30 * time.Minute),
			Backoff:            "exponential",
		},
	}
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	cfg, err := Decode(file)
	if err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

func Decode(reader io.Reader) (Config, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)

	cfg := Default()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("multiple YAML documents are not allowed")
		}
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Marshal(cfg Config) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

var placeholderPattern = regexp.MustCompile(`\{\{[^{}]+\}\}`)

var allowedPlaceholders = map[string]struct{}{
	"{{prompt}}":      {},
	"{{prompt_file}}": {},
	"{{workspace}}":   {},
	"{{task_id}}":     {},
	"{{attempt}}":     {},
	"{{model}}":       {},
}

func (cfg Config) Validate() error {
	if cfg.Version != 1 {
		return fmt.Errorf("version: must be 1")
	}
	if strings.TrimSpace(cfg.Server.Host) == "" {
		return fmt.Errorf("server.host: is required")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port: must be between 1 and 65535")
	}
	if cfg.Workers.Count < 1 {
		return fmt.Errorf("workers.count: must be at least 1")
	}

	runtimes := []struct {
		name string
		cfg  RuntimeConfig
	}{
		{"planner", cfg.Runtime.Planner},
		{"executor", cfg.Runtime.Executor},
		{"verifier", cfg.Runtime.Verifier},
	}
	for _, runtime := range runtimes {
		prefix := "runtime." + runtime.name
		if runtime.cfg.Adapter != "pi-cli" && runtime.cfg.Adapter != "command" {
			return fmt.Errorf("%s.adapter: must be pi-cli or command", prefix)
		}
		if strings.TrimSpace(runtime.cfg.Command) == "" {
			return fmt.Errorf("%s.command: is required", prefix)
		}
		if runtime.cfg.Timeout <= 0 {
			return fmt.Errorf("%s.timeout: must be positive", prefix)
		}
		for _, argument := range runtime.cfg.Args {
			for _, placeholder := range placeholderPattern.FindAllString(argument, -1) {
				if _, ok := allowedPlaceholders[placeholder]; !ok {
					return fmt.Errorf("%s.args: unknown placeholder %s", prefix, placeholder)
				}
			}
		}
	}

	if cfg.Retry.DefaultMaxAttempts < 1 {
		return fmt.Errorf("retry.default_max_attempts: must be at least 1")
	}
	if cfg.Retry.InitialDelay <= 0 || cfg.Retry.MaxDelay <= 0 {
		return fmt.Errorf("retry delays: must be positive")
	}
	if cfg.Retry.Backoff != "exponential" {
		return fmt.Errorf("retry.backoff: must be exponential")
	}
	if cfg.Loops.DefinitionRoot != "" && !filepath.IsAbs(cfg.Loops.DefinitionRoot) {
		return fmt.Errorf("loops.definition_root: must be an absolute path")
	}
	return nil
}
