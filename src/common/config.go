package common

import (
	"log"
	"os"
	"strconv"

	"gopkg.in/ini.v1"
)

var GetConfig = func() Configuration {
	daemonArr := []*Daemon{}
	var index int
	var configuration Configuration

	cfg, err := ini.Load("./conf/momo.conf")
	if err != nil {
		log.Printf(err.Error())
		os.Exit(1)
	}

	for {
		sec, err := cfg.GetSection("daemon." + strconv.Itoa(index))
		if err != nil {
			break
		}

		daemon := new(Daemon)
		daemon.Host = sec.Key("host").String()
		daemon.Chrep = sec.Key("change_replication").String()
		daemon.Data = sec.Key("data").String()
		daemon.Drive = sec.Key("drive").String()
		daemonArr = append(daemonArr, daemon)

		index = index + 1
	}

	configuration.Daemons = daemonArr

	configuration.Global.Debug, _ = strconv.ParseBool(cfg.Section("global").Key("debug").String())
	configuration.Global.ReplicationOrder = cfg.Section("global").Key("replication_order").String()
	configuration.Global.PolymorphicSystem, _ = strconv.ParseBool(cfg.Section("global").Key("polymorphic_system").String())

	configuration.Metrics.Interval, _ = strconv.Atoi(cfg.Section("metrics").Key("interval").String())
	configuration.Metrics.MinThreshold, _ = strconv.ParseFloat(cfg.Section("metrics").Key("min_threshold").String(), 64)
	configuration.Metrics.MaxThreshold, _ = strconv.ParseFloat(cfg.Section("metrics").Key("max_threshold").String(), 64)
	configuration.Metrics.FallbackInterval, _ = strconv.Atoi(cfg.Section("metrics").Key("fallback_interval").String())

	return configuration
}
