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
    Host string
    Chrep string
    Data string
    Drive string
}

type Configuration struct {
    Debug bool
    MetricsInterval int
    MaxThreshold float64
    MinThreshold float64
    Daemons []*Daemon
}
