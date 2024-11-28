package config

import (
	"errors"
	"fmt"
	"io"

	"github.com/databacker/api/go/api"
	"gopkg.in/yaml.v3"

	"github.com/databacker/mysql-backup/pkg/remote"
)

// ProcessConfig reads the configuration from a stream and returns the parsed configuration.
// If the configuration is of type remote, it will retrieve the remote configuration.
// Continues to process remotes until it gets a final valid ConfigSpec or fails.
func ProcessConfig(r io.Reader) (actualConfig *api.ConfigSpec, err error) {
	var conf api.Config
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&conf); err != nil {
		return nil, fmt.Errorf("fatal error reading config file: %w", err)
	}

	// check that the version is something we recognize
	if conf.Version != api.ConfigDatabackIoV1 {
		return nil, fmt.Errorf("unknown config version: %s", conf.Version)
	}
	specBytes, err := yaml.Marshal(conf.Spec)
	if err != nil {
		return nil, fmt.Errorf("error marshalling spec part of configuration: %w", err)
	}
	// if the config type is remote, retrieve our remote configuration
	// repeat until we end up with a configuration that is of type local
	for {
		switch conf.Kind {
		case api.Local:
			var spec api.ConfigSpec
			// there is a problem that api.ConfigSpec has json tags but not yaml tags.
			// This is because github.com/databacker/api uses oapi-codegen to generate the api
			// which creates json tags and not yaml tags. There is a PR to get them in.
			// http://github.com/oapi-codegen/oapi-codegen/pull/1798
			// Once that is in, and databacker/api uses them, this will work directly with yaml.
			// For now, because there are no yaml tags, it defaults to just lowercasing the
			// field. That means anything camelcase will be lowercased, which does not always
			// parse properly. For example, `thisField` will expect `thisfield` in the yaml, which
			// is incorrect.
			// We fix this by converting the spec part of the config into json,
			// as yaml is a valid subset of json, and then unmarshalling that.
			if err := yaml.Unmarshal(specBytes, &spec); err != nil {
				return nil, fmt.Errorf("parsed yaml had kind local, but spec invalid")
			}
			actualConfig = &spec
		case api.Remote:
			var spec api.RemoteSpec
			if err := yaml.Unmarshal(specBytes, &spec); err != nil {
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
func getRemoteConfig(spec api.RemoteSpec) (conf api.Config, err error) {
	if spec.URL == nil || spec.Certificates == nil || spec.Credentials == nil {
		return conf, errors.New("empty fields for components")
	}
	resp, err := remote.OpenConnection(*spec.URL, *spec.Certificates, *spec.Credentials)
	if err != nil {
		return conf, fmt.Errorf("error getting reader: %w", err)
	}
	defer resp.Body.Close()

	// Read the body of the response and convert to a config.Config struct
	var baseConf api.Config
	decoder := yaml.NewDecoder(resp.Body)
	if err := decoder.Decode(&baseConf); err != nil {
		return conf, fmt.Errorf("invalid config file retrieved from server: %w", err)
	}

	return baseConf, nil
}
