package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the structure of an anito.yaml file
type Config struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`         // "binary" | "static"
	Build       string `yaml:"build"`        // build command to run
	Output      string `yaml:"output"`       // path to resulting binary or static dir
	EnvFile     string `yaml:"env_file"`     // optional .env file
	HealthCheck string `yaml:"health_check"` // health check path (default /health)
}

// Load reads and parses an anito.yaml file
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
	if cfg.Output == "" {
		return nil, fmt.Errorf("%s: output is required", path)
	}
	if cfg.Type == "" {
		cfg.Type = "binary"
	}

	return &cfg, nil
}
