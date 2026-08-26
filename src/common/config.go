// Package common provides shared functionality for the momo application.
package common

import (
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"gopkg.in/ini.v1"
)

const (
	// sectionGlobal is the name of the [global] section in the configuration file.
	sectionGlobal = "global"
	// sectionMetrics is the name of the [metrics] section in the configuration file.
	sectionMetrics = "metrics"
	// sectionP2P is the name of the [p2p] section in the configuration file.
	sectionP2P = "p2p"
	// sectionStorage is the name of the [storage] section in the configuration file.
	sectionStorage = "storage"
	// prefixDaemon is the prefix for daemon sections in the configuration file (e.g., [daemon.0]).
	prefixDaemon = "daemon."
)

// Storage backend identifiers accepted by the [storage] section.
// These are shared with the blob-store factory (storage/factory.go) so the
// config-time validation and the runtime switch can never drift (issue #649).
const (
	BackendLocal = "local"
	BackendNFS   = "nfs"
	BackendS3    = "s3"
	BackendRaw   = "raw"
)

// defaultClientSideReplicationModes is the default when client_side_replication_modes
// is not specified in config. Defined at package level to avoid per-call allocation.
var defaultClientSideReplicationModes = []int{ReplicationPrimarySplay}

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

	// Parse client_side_replication_modes: comma-separated list of mode IDs that
	// require a momo-aware client. External S3 clients get these modes subtracted
	// from replication_order. Defaults to [3] (ReplicationPrimarySplay) if unset.
	clientSideStr := globalSec.Key("client_side_replication_modes").String()
	if clientSideStr != "" {
		csmCount := strings.Count(clientSideStr, ",") + 1
		modes := make([]int, 0, csmCount)
		for len(clientSideStr) > 0 {
			csmIdx := strings.IndexByte(clientSideStr, ',')
			var csmPart string
			if csmIdx == -1 {
				csmPart = clientSideStr
				clientSideStr = ""
			} else {
				csmPart = clientSideStr[:csmIdx]
				clientSideStr = clientSideStr[csmIdx+1:]
			}
			trimmedCSM := strings.TrimSpace(csmPart)
			if trimmedCSM == "" {
				continue
			}
			csmMode, err := strconv.Atoi(trimmedCSM)
			if err != nil {
				return Configuration{}, fmt.Errorf("failed to parse 'client_side_replication_modes' part %q: %w", trimmedCSM, err)
			}
			modes = append(modes, csmMode)
		}
		if len(modes) > 0 {
			config.Global.ClientSideReplicationModes = modes
		}
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

	// Resolve OPRF threshold default and validate against cluster size.
	// OPRFThreshold == 0 means "all daemons" (auto).
	if config.Global.OPRFThreshold == 0 {
		config.Global.OPRFThreshold = len(config.Daemons)
	}
	if config.Global.OPRFThreshold > len(config.Daemons) {
		return Configuration{}, fmt.Errorf("'oprf_threshold' %d exceeds number of daemons %d: %w", config.Global.OPRFThreshold, len(config.Daemons), syscall.EINVAL)
	}

	// Validate OPRF share provisioning when confidential dedup is enabled.
	// Each daemon must hold a distinct 256-bit Shamir share, and the share
	// index defaults to its position in the config when not set explicitly.
	if config.Global.OPRFEnabled {
		seen := make(map[int]struct{}, len(config.Daemons))
		for i, d := range config.Daemons {
			if d.OPRFShare == "" {
				return Configuration{}, fmt.Errorf("daemon %d is missing 'oprf_share' in [daemon.%d] while oprf is enabled: %w", i, i, syscall.EINVAL)
			}
			if len(d.OPRFShare) != 64 {
				return Configuration{}, fmt.Errorf("'oprf_share' for [daemon.%d] must be 64 hex characters (256-bit): %w", i, syscall.EINVAL)
			}
			if d.OPRFShareIndex == 0 {
				d.OPRFShareIndex = i + 1
			}
			if d.OPRFShareIndex < 1 || d.OPRFShareIndex > len(config.Daemons) {
				return Configuration{}, fmt.Errorf("'oprf_share_index' %d for [daemon.%d] out of range [1,%d]: %w", d.OPRFShareIndex, i, len(config.Daemons), syscall.EINVAL)
			}
			if _, dup := seen[d.OPRFShareIndex]; dup {
				return Configuration{}, fmt.Errorf("duplicate 'oprf_share_index' %d across daemons: %w", d.OPRFShareIndex, syscall.EINVAL)
			}
			seen[d.OPRFShareIndex] = struct{}{}
		}
	}

	// Load [p2p] section (optional, defaults to disabled)
	p2pSec, err := cfg.GetSection(sectionP2P)
	if err == nil {
		config.P2P, err = loadP2PConfig(p2pSec)
		if err != nil {
			return Configuration{}, fmt.Errorf("failed to load [%s] section: %w", sectionP2P, err)
		}
	}

	// A threshold > 1 requires P2P to gather peer evaluations.
	if config.Global.OPRFEnabled && config.Global.OPRFThreshold > 1 && !config.P2P.Enabled {
		return Configuration{}, fmt.Errorf("'oprf_threshold' > 1 requires [p2p] enabled: %w", syscall.EINVAL)
	}

	// Load [storage] section (optional, defaults to standard GC settings)
	storageSec, err := cfg.GetSection(sectionStorage)
	if err == nil {
		config.Storage, err = loadStorageConfig(storageSec)
		if err != nil {
			return Configuration{}, fmt.Errorf("failed to load [%s] section: %w", sectionStorage, err)
		}
	} else {
		config.Storage = defaultStorageConfig()
	}

	return config, nil
}

// getConfigFromFileMu protects getConfigFromFileFn from concurrent reads/writes (Rule 471).
var (
	getConfigFromFileMu sync.RWMutex
	getConfigFromFileFn = func() (Configuration, error) {
		return GetConfig("conf/momo.conf")
	}
)

// GetConfigFromFile loads the configuration from the default path "conf/momo.conf".
// It is thread-safe; tests may override the implementation via SetConfigFromFile.
func GetConfigFromFile() (Configuration, error) {
	getConfigFromFileMu.RLock()
	fn := getConfigFromFileFn
	getConfigFromFileMu.RUnlock()
	return fn()
}

// SetConfigFromFile overrides the config loader implementation (for testing).
func SetConfigFromFile(fn func() (Configuration, error)) {
	getConfigFromFileMu.Lock()
	getConfigFromFileFn = fn
	getConfigFromFileMu.Unlock()
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

	if len(globalCfg.ReplicationOrder) == 0 {
		return ConfigurationGlobal{}, fmt.Errorf("'replication_order' contains no valid entries: %w", syscall.EINVAL)
	}

	// Copy the default slice instead of aliasing it: the shared package-level
	// slice must never be mutated in place by config consumers (Rule 9).
	globalCfg.ClientSideReplicationModes = append([]int(nil), defaultClientSideReplicationModes...)

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

	// minimum_durability_factor: the floor on replica copies the metrics
	// controller may select under load. 0 (default) disables the floor.
	// Must be >= 0 and, when enabled, must not exceed the desired replication
	// factor (issue #822).
	if key, err := section.GetKey("minimum_durability_factor"); err == nil {
		if v, e := key.Int(); e == nil {
			if v < 0 {
				return ConfigurationGlobal{}, fmt.Errorf("'minimum_durability_factor' must be >= 0: %w", syscall.EINVAL)
			}
			if v > globalCfg.ReplicationFactor {
				return ConfigurationGlobal{}, fmt.Errorf("'minimum_durability_factor' %d exceeds 'replication_factor' %d: %w", v, globalCfg.ReplicationFactor, syscall.EINVAL)
			}
			globalCfg.MinimumDurabilityFactor = v
		}
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

	// auth_backoff_delay (ms): base delay for adaptive failed-auth backoff.
	// 0 (default) disables throttling.
	if key, err := section.GetKey("auth_backoff_delay"); err == nil {
		if v, e := key.Int(); e == nil {
			if v < 0 {
				return ConfigurationGlobal{}, fmt.Errorf("'auth_backoff_delay' must be >= 0: %w", syscall.EINVAL)
			}
			globalCfg.AuthBackoffDelay = v
		}
	}

	if key, err := section.GetKey("ca_cert"); err == nil {
		globalCfg.CACertPath = key.String()
	}

	if key, err := section.GetKey("tls_cert"); err == nil {
		globalCfg.TLSCertPath = key.String()
	}

	if key, err := section.GetKey("tls_key"); err == nil {
		globalCfg.TLSKeyPath = key.String()
	}

	if key, err := section.GetKey("tls_insecure"); err == nil {
		if v, e := key.Bool(); e == nil {
			globalCfg.TLSInsecure = v
		}
	}

	if key, err := section.GetKey("encryption_enabled"); err == nil {
		if v, e := key.Bool(); e == nil {
			globalCfg.EncryptionEnabled = v
		}
	}

	if key, err := section.GetKey("encryption_key"); err == nil {
		globalCfg.EncryptionKey = key.String()
	}

	if key, err := section.GetKey("encryption_tenant"); err == nil {
		globalCfg.EncryptionTenant = key.String()
	}

	if key, err := section.GetKey("e2ee_key"); err == nil {
		globalCfg.E2EEKey = key.String()
	}

	if key, err := section.GetKey("e2ee_key_id"); err == nil {
		globalCfg.E2EEKeyID = key.String()
	}

	if globalCfg.E2EEKey != "" {
		if len(globalCfg.E2EEKey) != 64 {
			return ConfigurationGlobal{}, fmt.Errorf("'e2ee_key' must be 64 hex characters (256-bit): %w", syscall.EINVAL)
		}
		if _, err := hex.DecodeString(globalCfg.E2EEKey); err != nil {
			return ConfigurationGlobal{}, fmt.Errorf("'e2ee_key' must be valid hex: %w", err)
		}
	}

	if globalCfg.E2EEKeyID == "" {
		globalCfg.E2EEKeyID = "default"
	}

	if globalCfg.EncryptionEnabled {
		if len(globalCfg.EncryptionKey) != 64 {
			return ConfigurationGlobal{}, fmt.Errorf("'encryption_key' must be 64 hex characters (256-bit) when encryption is enabled: %w", syscall.EINVAL)
		}
		if _, err := hex.DecodeString(globalCfg.EncryptionKey); err != nil {
			return ConfigurationGlobal{}, fmt.Errorf("'encryption_key' must be valid hex: %w", err)
		}
	}

	if globalCfg.EncryptionTenant == "" {
		globalCfg.EncryptionTenant = "default"
	}

	if key, err := section.GetKey("oprf_enabled"); err == nil {
		if v, e := key.Bool(); e == nil {
			globalCfg.OPRFEnabled = v
		}
	}

	if key, err := section.GetKey("oprf_threshold"); err == nil {
		if v, e := key.Int(); e == nil {
			if v < 1 {
				return ConfigurationGlobal{}, fmt.Errorf("'oprf_threshold' must be >= 1: %w", syscall.EINVAL)
			}
			globalCfg.OPRFThreshold = v
		}
	}

	// Envelope E2EE (client-held key) and OPRF confidential dedup share the
	// wire format's key-management slot; they are mutually exclusive in the
	// first iteration (issue #780).
	if globalCfg.E2EEKey != "" && globalCfg.OPRFEnabled {
		return ConfigurationGlobal{}, fmt.Errorf("'e2ee_key' (envelope E2EE) is mutually exclusive with OPRF confidential dedup: %w", syscall.EINVAL)
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
	if metricsCfg.FallbackInterval <= 0 {
		return ConfigurationMetrics{}, fmt.Errorf("'fallback_interval' must be positive, got %d: %w", metricsCfg.FallbackInterval, syscall.EINVAL)
	}

	metricsCfg.PrometheusPort, err = section.Key("prometheus_port").Int()
	if err != nil {
		metricsCfg.PrometheusPort = 0
	}

	metricsCfg.PrometheusBindHost = section.Key("prometheus_bind_host").String()

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
	if err != nil {
		p2pCfg.Fanout = 0
	}
	// fanout == 0 (default) means adaptive gossip fanout (ceil(ln N)), bounded.
	// A positive fanout is an explicit fixed override. Negative/zero is clamped
	// to adaptive (issue #825).
	if p2pCfg.Fanout < 0 {
		return ConfigurationP2P{}, fmt.Errorf("'fanout' must be >= 0 (0 = adaptive): %w", syscall.EINVAL)
	}

	p2pCfg.PingTimeout, err = section.Key("ping_timeout").Int()
	if err != nil || p2pCfg.PingTimeout <= 0 {
		p2pCfg.PingTimeout = 500
	}

	p2pCfg.IndirectPingCount, err = section.Key("indirect_ping_count").Int()
	if err != nil || p2pCfg.IndirectPingCount <= 0 {
		p2pCfg.IndirectPingCount = 3
	}
	if p2pCfg.IndirectPingCount > 10 {
		p2pCfg.IndirectPingCount = 10
	}

	p2pCfg.ScatterGatherTimeout, err = section.Key("scatter_gather_timeout").Int()
	if err != nil || p2pCfg.ScatterGatherTimeout <= 0 {
		p2pCfg.ScatterGatherTimeout = 5
	}

	p2pCfg.LeaseTimeout, err = section.Key("lease_timeout").Int()
	if err != nil || p2pCfg.LeaseTimeout <= 0 {
		p2pCfg.LeaseTimeout = 10
	}

	p2pCfg.TLSCertFile = section.Key("tls_cert_file").String()
	p2pCfg.TLSKeyFile = section.Key("tls_key_file").String()
	p2pCfg.TLSCAFile = section.Key("tls_ca_file").String()

	return p2pCfg, nil
}

// defaultStorageConfig returns the default storage configuration.
func defaultStorageConfig() ConfigurationStorage {
	return ConfigurationStorage{
		Backend:            BackendLocal,
		GCInterval:         300,
		TombstoneRetention: 86400,
		ScrubInterval:      3600,
		VerifyOnRead:       true,
	}
}

// loadStorageConfig loads the [storage] section from the configuration.
func loadStorageConfig(section *ini.Section) (ConfigurationStorage, error) {
	cfg := defaultStorageConfig()
	var err error

	cfg.Backend = section.Key("backend").String()
	if cfg.Backend == "" {
		cfg.Backend = BackendLocal
	}
	// 🛡️ Validate the backend eagerly so an invalid value (e.g. "foobar") fails
	// at config load time instead of surfacing as a runtime error later (issue #649).
	switch cfg.Backend {
	case BackendLocal, BackendNFS, BackendS3, BackendRaw:
	default:
		return ConfigurationStorage{}, fmt.Errorf("unsupported storage backend %q (valid: %s, %s, %s, %s): %w", cfg.Backend, BackendLocal, BackendNFS, BackendS3, BackendRaw, syscall.EINVAL)
	}

	cfg.GCInterval, err = section.Key("gc_interval").Int()
	if err != nil || cfg.GCInterval <= 0 {
		cfg.GCInterval = 300
	}

	cfg.TombstoneRetention, err = section.Key("tombstone_retention").Int()
	if err != nil || cfg.TombstoneRetention <= 0 {
		cfg.TombstoneRetention = 86400
	}

	cfg.ScrubInterval, err = section.Key("scrub_interval").Int()
	if err != nil || cfg.ScrubInterval <= 0 {
		cfg.ScrubInterval = 3600
	}

	cfg.VerifyOnRead, err = section.Key("verify_on_read").Bool()
	if err != nil {
		cfg.VerifyOnRead = true
	}

	cfg.S3Endpoint = section.Key("s3_endpoint").String()
	cfg.S3Region = section.Key("s3_region").String()
	cfg.S3Bucket = section.Key("s3_bucket").String()
	cfg.S3AccessKey = section.Key("s3_access_key").String()
	cfg.S3SecretKey = section.Key("s3_secret_key").String()
	cfg.S3ServerAccessKey = section.Key("s3_server_access_key").String()
	cfg.S3ServerSecretKey = section.Key("s3_server_secret_key").String()
	cfg.S3PathStyle, err = section.Key("s3_path_style").Bool()
	if err != nil {
		cfg.S3PathStyle = true
	}

	cfg.S3Insecure, err = section.Key("s3_insecure").Bool()
	if err != nil {
		cfg.S3Insecure = false
	}

	cfg.RawDevicePath = section.Key("raw_device_path").String()

	// issue #656: dedicated S3 gateway SigV4 credentials must be set as a pair
	// and stay within the wire field bounds (PadString rejects >64 bytes).
	if (cfg.S3ServerAccessKey == "") != (cfg.S3ServerSecretKey == "") {
		return ConfigurationStorage{}, fmt.Errorf("'s3_server_access_key' and 's3_server_secret_key' must be configured together: %w", syscall.EINVAL)
	}
	if len(cfg.S3ServerAccessKey) > AuthTokenLength {
		return ConfigurationStorage{}, fmt.Errorf("'s3_server_access_key' length exceeds maximum allowed length of %d bytes", AuthTokenLength)
	}
	if len(cfg.S3ServerSecretKey) > AuthTokenLength {
		return ConfigurationStorage{}, fmt.Errorf("'s3_server_secret_key' length exceeds maximum allowed length of %d bytes", AuthTokenLength)
	}

	return cfg, nil
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

		keys := make([]string, 0, len(requiredFields))
		for key := range requiredFields {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			ptr := requiredFields[key]
			*ptr = section.Key(key).String()
			if *ptr == "" {
				return nil, fmt.Errorf("missing '%s' in section [%s]", key, sectionName)
			}
		}

		d.OPRFShare = section.Key("oprf_share").String()
		if d.OPRFShare != "" {
			if _, err := hex.DecodeString(d.OPRFShare); err != nil {
				return nil, fmt.Errorf("invalid 'oprf_share' in section [%s]: must be valid hex: %w", sectionName, err)
			}
		}
		if key, err := section.GetKey("oprf_share_index"); err == nil {
			if v, e := key.Int(); e == nil {
				d.OPRFShareIndex = v
			}
		}

		d.MetricsBindHost = section.Key("metrics_host").String()

		// failure_domain is optional (R1, #929); empty means the daemon is
		// unclassified and shares the default failure domain with every other
		// unclassified node.
		d.FailureDomain = section.Key("failure_domain").String()

		if key, err := section.GetKey("metrics_port"); err == nil {
			v, e := key.Int()
			if e != nil {
				return nil, fmt.Errorf("invalid 'metrics_port' in section [%s]: %v: %w", sectionName, e, syscall.EINVAL)
			}
			if v < 1 || v > 65535 {
				return nil, fmt.Errorf("invalid 'metrics_port' %d in section [%s]: must be in [1, 65535]: %w", v, sectionName, syscall.EINVAL)
			}
			d.MetricsBindPort = v
		}

		daemons = append(daemons, d)
	}

	if len(daemons) == 0 {
		return nil, fmt.Errorf("no [%s*] sections found in configuration", prefixDaemon)
	}

	return daemons, nil
}
