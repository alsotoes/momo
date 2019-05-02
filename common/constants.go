package common

const (
    // Socket constants
    BUFFERSIZE = 1024
    LENGTHTIMESTAMP = 19
    LENGTHINFO = 64

    // Replication types
    NO_REPLICATION = 0
    CHAIN_REPLICATION = 1
    SPLAY_REPLICATION = 2
    PRIMARY_SPLAY_REPLICATION = 3
)

var (
    // ReplicationMode at stating point
    ReplicationMode = 1

    // Struct to lookback states when changing replication mode
    ReplicationLookBack = &ReplicationData{
        Old: ReplicationMode,
        New: ReplicationMode,
        TimeStamp: 1}
)
