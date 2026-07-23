// Package common provides shared functionality for the momo application.
package common

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

const (
	// sectionGlobal is the name of the [global] section in the configuration file.
	sectionGlobal = "global"
	// sectionMetrics is the name of the [metrics] section in the configuration file.
	sectionMetrics = "metrics"
	// sectionP2P is the name of the [p2p] section in the configuration file.
	sectionP2P = "p2p"
	// prefixDaemon is the prefix for daemon sections in the configuration file (e.g., [daemon.0]).
	prefixDaemon = "daemon."
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

	// Load [p2p] section (optional, defaults to disabled)
	p2pSec, err := cfg.GetSection(sectionP2P)
	if err == nil {
		config.P2P, err = loadP2PConfig(p2pSec)
		if err != nil {
			return Configuration{}, fmt.Errorf("failed to load [%s] section: %w", sectionP2P, err)
		}
	}

	return config, nil
}

// GetConfigFromFile is a function variable that loads the configuration from the default path "conf/momo.conf".
// It can be overridden in tests to load a custom configuration for testing purposes.
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

	globalCfg.AuthToken = section.Key("auth_token").String()
	if globalCfg.AuthToken == "" {
		return ConfigurationGlobal{}, fmt.Errorf("'auth_token' is missing or empty")
	}

	replicationOrderStr := section.Key("replication_order").String()
	if replicationOrderStr == "" {
		return ConfigurationGlobal{}, fmt.Errorf("'replication_order' is missing or empty")
	}

	// ⚡ Bolt: Use a zero-allocation loop instead of strings.Split to parse replication_order.
	// We pre-calculate the capacity using strings.Count to avoid re-allocations.
	count := strings.Count(replicationOrderStr, ",") + 1
	globalCfg.ReplicationOrder = make([]int, 0, count)
	for len(replicationOrderStr) > 0 {

		idx := strings.IndexByte(replicationOrderStr, ',')
		var part string
		if idx == -1 {
			part = replicationOrderStr
			replicationOrderStr = ""
		} else {
			part = replicationOrderStr[:idx]
			replicationOrderStr = replicationOrderStr[idx+1:]
		}

		trimmedPart := strings.TrimSpace(part)
		if trimmedPart == "" {
			continue
		}
		// 🛡️ Zero-Crash Hardening: strconv.Atoi is safe for validated config strings.
		order, err := strconv.Atoi(trimmedPart)
		if err != nil {
			return ConfigurationGlobal{}, fmt.Errorf("failed to parse 'replication_order' part %q: %w", trimmedPart, err)
		}
		globalCfg.ReplicationOrder = append(globalCfg.ReplicationOrder, order)
	}

	globalCfg.PolymorphicSystem, err = section.Key("polymorphic_system").Bool()
	if err != nil {
		return ConfigurationGlobal{}, fmt.Errorf("failed to parse 'polymorphic_system': %w", err)
	}

	replicationFactorKey, err := section.GetKey("replication_factor")
	if err != nil {
		log.Printf("WARNING: No replication_factor found, defaulting to 3")
		globalCfg.ReplicationFactor = 3
	} else {
		factor, err := replicationFactorKey.Int()
		if err != nil || factor < 1 {
			return ConfigurationGlobal{}, fmt.Errorf("invalid 'replication_factor': must be an integer >= 1")
		}
		globalCfg.ReplicationFactor = factor
	}

	protocolKey, err := section.GetKey("protocol")
	if err != nil {
		log.Printf("WARNING: No protocol definition found, falling back to default (momo-tcp)")
		globalCfg.Protocol = "momo-tcp"
	} else {
		protocolStr := protocolKey.String()
		switch protocolStr {
		case "momo-tcp", "momo-quic", "s3-tcp", "s3-quic":
			globalCfg.Protocol = protocolStr
		default:
			return ConfigurationGlobal{}, fmt.Errorf("invalid or unsupported protocol: %q", protocolStr)
		}
	}

	// 🛡️ Sentinel: Fail securely if the AuthToken exceeds the maximum allowed length (64 bytes).
	// Silently truncating long tokens reduces their effective entropy and can hide configuration errors.
	if len(globalCfg.AuthToken) > AuthTokenLength {
		return ConfigurationGlobal{}, fmt.Errorf("'auth_token' length exceeds maximum allowed length of %d bytes", AuthTokenLength)
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

// loadP2PConfig loads the [p2p] section from the configuration.
func loadP2PConfig(section *ini.Section) (ConfigurationP2P, error) {
	var p2pCfg ConfigurationP2P
	var err error

	p2pCfg.Enabled, err = section.Key("enabled").Bool()
	if err != nil {
		p2pCfg.Enabled = false
	}

	p2pCfg.GossipPort = section.Key("gossip_port").String()
	if p2pCfg.GossipPort == "" {
		p2pCfg.GossipPort = "4450"
	}

	p2pCfg.GossipInterval, err = section.Key("gossip_interval").Int()
	if err != nil || p2pCfg.GossipInterval <= 0 {
		p2pCfg.GossipInterval = 1
	}

	p2pCfg.SuspicionTimeout, err = section.Key("suspicion_timeout").Int()
	if err != nil || p2pCfg.SuspicionTimeout <= 0 {
		p2pCfg.SuspicionTimeout = 5
	}

	p2pCfg.Fanout, err = section.Key("fanout").Int()
	if err != nil || p2pCfg.Fanout <= 0 {
		p2pCfg.Fanout = 3
	}

	return p2pCfg, nil
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
			"host":               &d.Host,
			"change_replication": &d.ChangeReplication,
			"data":               &d.Data,
			"drive":              &d.Drive,
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
