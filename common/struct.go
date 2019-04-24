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
