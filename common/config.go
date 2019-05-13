package common

import (
    "os"
    "log"
    "strconv"

    "gopkg.in/ini.v1"
)

func GetConfig() Configuration {

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
        daemon.Chrep = sec.Key("chrep").String()
        daemon.Data = sec.Key("data").String()
        daemon.Drive = sec.Key("drive").String()
        daemonArr = append(daemonArr, daemon)

        index = index +1

    }

    configuration.Debug, _ = strconv.ParseBool(cfg.Section("global").Key("debug").String())
    configuration.MetricsInterval, _ = strconv.Atoi(cfg.Section("metrics").Key("interval").String())
    configuration.MinThreshold, _ = strconv.ParseFloat(cfg.Section("metrics").Key("min_threshold").String(), 64)
    configuration.MaxThreshold, _ = strconv.ParseFloat(cfg.Section("metrics").Key("max_threshold").String(), 64)
    configuration.Daemons = daemonArr

    return configuration
}
