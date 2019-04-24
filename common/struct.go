package common

type FileMetadata struct {
    Name string
    MD5  string
    Size int64
}

type ReplicationData struct {
    OldReplication int
    NewReplication int
    TimeStamp int64
}
