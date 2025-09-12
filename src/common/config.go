package common

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

const (
	sectionGlobal  = "global"
	sectionMetrics = "metrics"
	prefixDaemon   = "daemon."
)

// GetConfig loads and validates the configuration from the given file path.
func GetConfig(path string) (Configuration, error) {
	var config Configuration

	cfg, err := ini.Load(path)
	if err != nil {
		return Configuration{}, fmt.Errorf("failed to load configuration file %q: %w", path, err)
	}

	// Load [global] section
	globalSec, err := cfg.GetSection(sectionGlobal)
	if err != nil {
		return Configuration{}, fmt.Errorf("configuration section [%s] not found in %q", sectionGlobal, path)
	}
	config.Global, err = loadGlobalConfig(globalSec)
	if err != nil {
		return Configuration{}, fmt.Errorf("failed to load [%s] section: %w", sectionGlobal, err)
	}

	// Load [metrics] section
	metricsSec, err := cfg.GetSection(sectionMetrics)
	if err != nil {
		return Configuration{}, fmt.Errorf("configuration section [%s] not found in %q", sectionMetrics, path)
	}
	config.Metrics, err = loadMetricsConfig(metricsSec)
	if err != nil {
		return Configuration{}, fmt.Errorf("failed to load [%s] section: %w", sectionMetrics, err)
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
		return ConfigurationGlobal{}, fmt.Errorf("failed to parse 'debug': %w", err)
	}

	replicationOrderStr := section.Key("replication_order").String()
	if replicationOrderStr == "" {
		return ConfigurationGlobal{}, fmt.Errorf("'replication_order' is missing or empty")
	}

	parts := strings.Split(replicationOrderStr, ",")
	for _, part := range parts {
		order, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return ConfigurationGlobal{}, fmt.Errorf("failed to parse 'replication_order': %w", err)
		}
		globalCfg.ReplicationOrder = append(globalCfg.ReplicationOrder, order)
	}

	globalCfg.PolymorphicSystem, err = section.Key("polymorphic_system").Bool()
	if err != nil {
		return ConfigurationGlobal{}, fmt.Errorf("failed to parse 'polymorphic_system': %w", err)
	}

	return globalCfg, nil
}

// loadMetricsConfig loads the [metrics] section from the configuration.
func loadMetricsConfig(section *ini.Section) (ConfigurationMetrics, error) {
	var metricsCfg ConfigurationMetrics
	var err error

	metricsCfg.Interval, err = section.Key("interval").Int()
	if err != nil {
		return ConfigurationMetrics{}, fmt.Errorf("failed to parse 'interval': %w", err)
	}

	metricsCfg.MinThreshold, err = section.Key("min_threshold").Float64()
	if err != nil {
		return ConfigurationMetrics{}, fmt.Errorf("failed to parse 'min_threshold': %w", err)
	}

	metricsCfg.MaxThreshold, err = section.Key("max_threshold").Float64()
	if err != nil {
		return ConfigurationMetrics{}, fmt.Errorf("failed to parse 'max_threshold': %w", err)
	}

	metricsCfg.FallbackInterval, err = section.Key("fallback_interval").Int()
	if err != nil {
		return ConfigurationMetrics{}, fmt.Errorf("failed to parse 'fallback_interval': %w", err)
	}

	return metricsCfg, nil
}

// loadDaemons loads all [daemon.*] sections from the configuration.
func loadDaemons(cfg *ini.File) ([]*Daemon, error) {
	var daemons []*Daemon
	daemonSections := cfg.SectionStrings()

	for _, sectionName := range daemonSections {
		if !strings.HasPrefix(sectionName, prefixDaemon) {
			continue
		}

		section, err := cfg.GetSection(sectionName)
		if err != nil {
			// This should not happen as we are iterating over existing sections
			return nil, fmt.Errorf("unexpected error getting section %s", sectionName)
		}

		d := &Daemon{}
		requiredFields := map[string]*string{
			"host":   &d.Host,
			"change_replication": &d.ChangeReplication,
			"data":   &d.Data,
			"drive":  &d.Drive,
		}

		for key, ptr := range requiredFields {
			*ptr = section.Key(key).String()
			if *ptr == "" {
				return nil, fmt.Errorf("missing '%s' in section [%s]", key, sectionName)
			}
		}

		daemons = append(daemons, d)
	}

	if len(daemons) == 0 {
		return nil, fmt.Errorf("no [%s*] sections found in configuration", prefixDaemon)
	}

	return daemons, nil
}
