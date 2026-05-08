// Package connector defines the Foglight connector SDK.
package connector

import "gopkg.in/yaml.v3"

// Config is the contract every connector's config struct must satisfy.
// Implementations should use a value receiver so the zero value satisfies the interface.
type Config interface {
	Validate() error
}

// DecodeConfig decodes a YAML node into C and runs Validate.
// An empty node yields the zero value of C, which is then validated.
func DecodeConfig[C Config](node yaml.Node) (C, error) {
	var cfg C
	if len(node.Content) > 0 {
		if err := node.Decode(&cfg); err != nil {
			return cfg, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}
