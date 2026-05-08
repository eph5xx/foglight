package gateway

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Connectors []ConnectorEntry `yaml:"connectors"`
}

type ConnectorEntry struct {
	Name   string    `yaml:"name"`
	Config yaml.Node `yaml:"config"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &c, nil
}
