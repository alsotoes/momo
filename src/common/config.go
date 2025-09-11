package common

import (
	"log"
	"strconv"

	"gopkg.in/ini.v1"
)

// GetConfig loads the configuration from the given file path.
func GetConfig(path string) (Configuration, error) {
	var configuration Configuration
	cfg, err := ini.Load(path)
	if err != nil {
		log.Printf("Failed to load configuration: %v", err)
		return configuration, err
	}

	// Load daemons dynamically
	daemonArr := []*Daemon{}
	index := 0
	for {
		sec, err := cfg.GetSection("daemon." + strconv.Itoa(index))
		if err != nil {
			break // No more daemon sections
		}

		daemon := &Daemon{
			Host:  sec.Key("host").String(),
			Chrep: sec.Key("change_replication").String(),
			Data:  sec.Key("data").String(),
			Drive: sec.Key("drive").String(),
		}
		daemonArr = append(daemonArr, daemon)
		index++
	}
	configuration.Daemons = daemonArr

	// Load global settings
	globalSec := cfg.Section("global")
	configuration.Global.Debug, _ = globalSec.Key("debug").Bool()
	configuration.Global.ReplicationOrder = globalSec.Key("replication_order").String()
	configuration.Global.PolymorphicSystem, _ = globalSec.Key("polymorphic_system").Bool()

	// Load metrics settings
	metricsSec := cfg.Section("metrics")
	configuration.Metrics.Interval, _ = metricsSec.Key("interval").Int()
	configuration.Metrics.MinThreshold, _ = metricsSec.Key("min_threshold").Float64()
	configuration.Metrics.MaxThreshold, _ = metricsSec.Key("max_threshold").Float64()
	configuration.Metrics.FallbackInterval, _ = metricsSec.Key("fallback_interval").Int()

	return configuration, nil
}

// GetConfigFromFile loads the configuration from the default path.
var GetConfigFromFile = func() (Configuration, error) {
	return GetConfig("conf/momo.conf")
}
