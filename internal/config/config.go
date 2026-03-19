package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the structure of a .anito/config.yaml file.
type Config struct {
	Name        string        `yaml:"name"`
	Version     string        `yaml:"version"`      // optional — semantic version tag, e.g. "v1.2.3"
	Port        int           `yaml:"port"`         // stable port consumers connect to (0 = auto-allocate)
	Type        string        `yaml:"type"`         // "binary" | "static" (default: binary)
	Build       string        `yaml:"build"`        // build command to run before deploying
	Output      string        `yaml:"output"`       // path to resulting binary or static dir
	Args        []string      `yaml:"args"`         // optional arguments passed to the binary at startup
	EnvFile     string        `yaml:"env_file"`     // optional .env file
	HealthCheck string        `yaml:"health_check"` // health check path (default: /health)
	Watch              []string      `yaml:"watch"`               // directories to watch for file changes (triggers restart)
	DrainWindow        time.Duration `yaml:"drain_window"`        // grace period between proxy swap and SIGTERM to old process (e.g. 3s)
	HealthCheckTimeout time.Duration `yaml:"health_check_timeout"` // how long to poll /health before giving up (0 = default 15s)
	RestartPolicy      string        `yaml:"restart_policy"`      // "always" | "on-watch" | "never" (default: "on-watch")
}

// Load reads and parses a .anito/config.yaml file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if cfg.Name == "" {
		return nil, fmt.Errorf("%s: name is required", path)
	}
	// port is optional — 0 means Anito auto-allocates from its port range.
	if cfg.Output == "" {
		return nil, fmt.Errorf("%s: output is required", path)
	}
	if cfg.Type == "" {
		cfg.Type = "binary"
	}
	if cfg.HealthCheck == "" {
		cfg.HealthCheck = "/health"
	}
	if cfg.RestartPolicy == "" {
		cfg.RestartPolicy = "on-watch"
	}
	switch cfg.RestartPolicy {
	case "always", "on-watch", "never":
	default:
		return nil, fmt.Errorf("%s: restart_policy must be \"always\", \"on-watch\", or \"never\"", path)
	}

	// Resolve relative Watch paths against the config file's directory so that
	// configs checked into a repo work on any machine without absolute paths.
	configDir := filepath.Dir(path)
	for i, w := range cfg.Watch {
		if !filepath.IsAbs(w) {
			cfg.Watch[i] = filepath.Join(configDir, w)
		}
	}

	return &cfg, nil
}
