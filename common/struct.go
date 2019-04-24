package common

type FileMetadata struct {
    Name string
    MD5  string
    Size int64
}

type ReplicationData struct {
    OldReplication int  `json:"oldreplication"`
    NewReplication int  `json:"newreplication"`
    TimeStamp int64     `json:"timestamp"`
}

type Daemon struct {
    Host string
    Chrep string
    Data string
}

type Configuration struct {
    Debug bool
    MetricsInterval int
    MetricsHost string
    Daemons []*Daemon
}
