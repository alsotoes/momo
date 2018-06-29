package common

import (
    "os"
    "log"
    "strconv"

    "gopkg.in/ini.v1"
)

type Daemon struct {
    Host string
    Metric string
    Data string
}

type Configuration struct {
    Debug bool
    MetricsInterval int
    MetricsHost string
    Daemons []*Daemon
}

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
        daemon.Metric = sec.Key("metric").String()
        daemon.Data = sec.Key("data").String()
        daemonArr = append(daemonArr, daemon)

        index = index +1

    }

    configuration.Debug, _ = strconv.ParseBool(cfg.Section("global").Key("debug").String())
    configuration.MetricsInterval, _ = strconv.Atoi(cfg.Section("global").Key("metrics_interval").String())
    configuration.MetricsHost = cfg.Section("global").Key("metrics_host").String()
    configuration.Daemons = daemonArr

    return configuration

}
