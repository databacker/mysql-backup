package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type logLevel string

//nolint:unused // we expect to use these going forward
const (
	logLevelError   logLevel = "error"
	logLevelWarning logLevel = "warning"
	logLevelInfo    logLevel = "info"
	logLevelDebug   logLevel = "debug"
	logLevelTrace   logLevel = "trace"
	logLevelDefault logLevel = logLevelInfo

	ConfigVersion = "config.databack.io/v1"
	KindLocal     = "local"
	KindRemote    = "remote"
)

type Config struct {
	Kind     string   `yaml:"kind"`
	Version  string   `yaml:"version"`
	Metadata Metadata `yaml:"metadata"`
	Spec     any      `yaml:"spec"`
}

var _ yaml.Unmarshaler = &Config{}

type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Digest      string `yaml:"digest"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface, so we can handle the Spec correctly
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	type T struct {
		Kind     string    `yaml:"kind"`
		Version  string    `yaml:"version"`
		Metadata Metadata  `yaml:"metadata"`
		Spec     yaml.Node `yaml:"spec"`
	}
	obj := &T{}
	if err := n.Decode(obj); err != nil {
		return err
	}
	switch obj.Kind {
	case KindLocal:
		// parse the config.Spec
		var spec ConfigSpec
		if err := obj.Spec.Decode(&spec); err != nil {
			return err
		}
		c.Spec = spec
	case KindRemote:
		var spec RemoteSpec
		if err := obj.Spec.Decode(&spec); err != nil {
			return err
		}
		c.Spec = spec
	default:
		return fmt.Errorf("unknown config type: %s", obj.Kind)
	}
	c.Kind = obj.Kind
	c.Version = obj.Version
	c.Metadata = obj.Metadata
	return nil
}
