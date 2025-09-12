package common

type FileMetadata struct {
    Name string
    MD5  string
    Size int64
}

type ReplicationData struct {
    Old int         `json:"old"`
    New int         `json:"new"`
    TimeStamp int64 `json:"timestamp"`
}

type Daemon struct {
    Host             string
    ChangeReplication string
    Data             string
    Drive            string
}

type ConfigurationGlobal struct {
    Debug             bool
    ReplicationOrder  []int
    PolymorphicSystem bool
}

type ConfigurationMetrics struct {
    Interval         int
    MaxThreshold     float64
    MinThreshold     float64
    FallbackInterval int
}

type Configuration struct {
    Daemons []*Daemon
    Global  ConfigurationGlobal
    Metrics ConfigurationMetrics
}
