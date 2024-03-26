package config

import (
	"fmt"
	"io"

	"github.com/databacker/mysql-backup/pkg/remote"

	"gopkg.in/yaml.v3"
)

// ProcessConfig reads the configuration from a stream and returns the parsed configuration.
// If the configuration is of type remote, it will retrieve the remote configuration.
// Continues to process remotes until it gets a final valid ConfigSpec or fails.
func ProcessConfig(r io.Reader) (actualConfig *ConfigSpec, err error) {
	var conf Config
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&conf); err != nil {
		return nil, fmt.Errorf("fatal error reading config file: %w", err)
	}

	// check that the version is something we recognize
	if conf.Version != ConfigVersion {
		return nil, fmt.Errorf("unknown config version: %s", conf.Version)
	}
	// if the config type is remote, retrieve our remote configuration
	// repeat until we end up with a configuration that is of type local
	for {
		switch conf.Kind {
		case KindLocal:
			// parse the config.Config
			spec, ok := conf.Spec.(ConfigSpec)
			if !ok {
				return nil, fmt.Errorf("parsed yaml had kind local, but spec invalid")
			}
			actualConfig = &spec
		case KindRemote:
			spec, ok := conf.Spec.(RemoteSpec)
			if !ok {
				return nil, fmt.Errorf("parsed yaml had kind remote, but spec invalid")
			}
			remoteConfig, err := getRemoteConfig(spec)
			if err != nil {
				return nil, fmt.Errorf("error parsing remote config: %w", err)
			}
			conf = remoteConfig
		default:
			return nil, fmt.Errorf("unknown config type: %s", conf.Kind)
		}
		if actualConfig != nil {
			break
		}
	}
	return actualConfig, nil
}

// getRemoteConfig given a RemoteSpec for a config, retrieve the config from the remote
// and parse it into a Config struct.
func getRemoteConfig(spec RemoteSpec) (conf Config, err error) {
	resp, err := remote.OpenConnection(spec.URL, spec.Certificates, spec.Credentials)
	if err != nil {
		return conf, fmt.Errorf("error getting reader: %w", err)
	}
	defer resp.Body.Close()

	// Read the body of the response and convert to a config.Config struct
	var baseConf Config
	decoder := yaml.NewDecoder(resp.Body)
	if err := decoder.Decode(&baseConf); err != nil {
		return conf, fmt.Errorf("invalid config file retrieved from server: %w", err)
	}

	return baseConf, nil
}
