package common

import (
	"fmt"
	"strings"

	"gopkg.in/ini.v1"
)

// GetConfig loads and validates the configuration from the given file path.
func GetConfig(path string) (Configuration, error) {
	var config Configuration

	cfg, err := ini.Load(path)
	if err != nil {
		return Configuration{}, fmt.Errorf("failed to load configuration file %q: %w", path, err)
	}

	// Load [global] section
	globalSec, err := cfg.GetSection("global")
	if err != nil {
		return Configuration{}, fmt.Errorf("configuration section [global] not found in %q", path)
	}
	config.Global, err = loadGlobalConfig(globalSec)
	if err != nil {
		return Configuration{}, err
	}

	// Load [metrics] section
	metricsSec, err := cfg.GetSection("metrics")
	if err != nil {
		return Configuration{}, fmt.Errorf("configuration section [metrics] not found in %q", path)
	}
	config.Metrics, err = loadMetricsConfig(metricsSec)
	if err != nil {
		return Configuration{}, err
	}

	// Load [daemon.*] sections
	config.Daemons, err = loadDaemons(cfg)
	if err != nil {
		return Configuration{}, err
	}

	return config, nil
}

// GetConfigFromFile is a variable that can be overridden for testing.
var GetConfigFromFile = func() (Configuration, error) {
	return GetConfig("conf/momo.conf")
}

// loadGlobalConfig loads the [global] section from the configuration.
func loadGlobalConfig(section *ini.Section) (ConfigurationGlobal, error) {
	var globalCfg ConfigurationGlobal
	var err error

	globalCfg.Debug, err = section.Key("debug").Bool()
	if err != nil {
		return ConfigurationGlobal{}, fmt.Errorf("failed to parse 'debug' in [global] section: %w", err)
	}

	globalCfg.ReplicationOrder = section.Key("replication_order").String()
	if globalCfg.ReplicationOrder == "" {
		return ConfigurationGlobal{}, fmt.Errorf("'replication_order' in [global] section is missing or empty")
	}

	globalCfg.PolymorphicSystem, err = section.Key("polymorphic_system").Bool()
	if err != nil {
		return ConfigurationGlobal{}, fmt.Errorf("failed to parse 'polymorphic_system' in [global] section: %w", err)
	}

	return globalCfg, nil
}

// loadMetricsConfig loads the [metrics] section from the configuration.
func loadMetricsConfig(section *ini.Section) (ConfigurationMetrics, error) {
	var metricsCfg ConfigurationMetrics
	var err error

	metricsCfg.Interval, err = section.Key("interval").Int()
	if err != nil {
		return ConfigurationMetrics{}, fmt.Errorf("failed to parse 'interval' in [metrics] section: %w", err)
	}

	metricsCfg.MinThreshold, err = section.Key("min_threshold").Float64()
	if err != nil {
		return ConfigurationMetrics{}, fmt.Errorf("failed to parse 'min_threshold' in [metrics] section: %w", err)
	}

	metricsCfg.MaxThreshold, err = section.Key("max_threshold").Float64()
	if err != nil {
		return ConfigurationMetrics{}, fmt.Errorf("failed to parse 'max_threshold' in [metrics] section: %w", err)
	}

	metricsCfg.FallbackInterval, err = section.Key("fallback_interval").Int()
	if err != nil {
		return ConfigurationMetrics{}, fmt.Errorf("failed to parse 'fallback_interval' in [metrics] section: %w", err)
	}

	return metricsCfg, nil
}

// loadDaemons loads all [daemon.*] sections from the configuration.
func loadDaemons(cfg *ini.File) ([]*Daemon, error) {
	var daemons []*Daemon
	for _, section := range cfg.Sections() {
		if !strings.HasPrefix(section.Name(), "daemon.") {
			continue
		}

		daemon := &Daemon{
			Host:  section.Key("host").String(),
			Chrep: section.Key("change_replication").String(),
			Data:  section.Key("data").String(),
			Drive: section.Key("drive").String(),
		}

		if daemon.Host == "" {
			return nil, fmt.Errorf("missing 'host' in section %s", section.Name())
		}
		if daemon.Chrep == "" {
			return nil, fmt.Errorf("missing 'change_replication' in section %s", section.Name())
		}
		if daemon.Data == "" {
			return nil, fmt.Errorf("missing 'data' in section %s", section.Name())
		}
		if daemon.Drive == "" {
			return nil, fmt.Errorf("missing 'drive' in section %s", section.Name())
		}
		daemons = append(daemons, daemon)
	}

	if len(daemons) == 0 {
		return nil, fmt.Errorf("no [daemon.*] sections found in configuration")
	}

	return daemons, nil
}
