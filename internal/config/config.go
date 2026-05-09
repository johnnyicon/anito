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
	Name               string         `yaml:"name"`
	Version            string         `yaml:"version"`              // optional — semantic version tag, e.g. "v1.2.3"
	VersionPath        string         `yaml:"version_path"`         // optional path to hash when version is omitted
	Port               int            `yaml:"port"`                 // stable port consumers connect to (0 = auto-allocate); mutually exclusive with Ports
	Ports              map[string]int `yaml:"ports"`                // named ports for multi-port services; mutually exclusive with Port
	Type               string         `yaml:"type"`                 // "binary" | "static" (default: binary)
	Build              string         `yaml:"build"`                // build command to run before deploying
	Output             string         `yaml:"output"`               // path to resulting binary or static dir
	Args               []string       `yaml:"args"`                 // optional arguments passed to the binary at startup
	ProxyBindAddress   string         `yaml:"proxy_bind_address"`   // stable proxy listener address (default: localhost)
	EnvFile            string         `yaml:"env_file"`             // optional .env file
	HealthCheck        string         `yaml:"health_check"`         // health check path (default: /health)
	HealthCheckPort    string         `yaml:"health_check_port"`    // which named port to health-check (for multi-port; default: first port)
	Watch              []string       `yaml:"watch"`                // directories to watch for file changes (triggers restart)
	DrainWindow        time.Duration  `yaml:"drain_window"`         // grace period between proxy swap and SIGTERM to old process (e.g. 3s)
	HealthCheckTimeout time.Duration  `yaml:"health_check_timeout"` // how long to poll /health before giving up (0 = default 15s)
	RestartPolicy      string         `yaml:"restart_policy"`       // "always" | "on-watch" | "never" (default: "on-watch")
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
	// port and ports are mutually exclusive.
	if cfg.Port != 0 && len(cfg.Ports) > 0 {
		return nil, fmt.Errorf("%s: port and ports are mutually exclusive — use one or the other", path)
	}
	// Normalize: singular port → map form for uniform downstream handling.
	if cfg.Port != 0 && len(cfg.Ports) == 0 {
		cfg.Ports = map[string]int{"default": cfg.Port}
	}
	// Validate health_check_port references a valid port name.
	if cfg.HealthCheckPort != "" && len(cfg.Ports) > 0 {
		if _, ok := cfg.Ports[cfg.HealthCheckPort]; !ok {
			return nil, fmt.Errorf("%s: health_check_port %q does not match any key in ports", path, cfg.HealthCheckPort)
		}
	}
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

	// Resolve relative paths against the config file's directory so that
	// configs checked into a repo work on any machine without absolute paths.
	// Relative paths stored in the daemon registry would otherwise be resolved
	// against the daemon's working directory, not the repo root.
	configDir := filepath.Dir(path)
	if cfg.EnvFile != "" && !filepath.IsAbs(cfg.EnvFile) {
		cfg.EnvFile = filepath.Join(configDir, cfg.EnvFile)
	}
	if cfg.VersionPath != "" && !filepath.IsAbs(cfg.VersionPath) {
		cfg.VersionPath = filepath.Join(configDir, cfg.VersionPath)
	}
	for i, w := range cfg.Watch {
		if !filepath.IsAbs(w) {
			cfg.Watch[i] = filepath.Join(configDir, w)
		}
	}

	return &cfg, nil
}
