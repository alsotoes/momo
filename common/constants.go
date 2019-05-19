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

    // Dummy
    DUMMY_EPOCH = 1557906926566451195
)

var (
    // Replication mode to be changed concurrently
    ReplicationMode = 1

    // Struct to lookback states when changing replication mode
    ReplicationLookBack = &ReplicationData{
        Old: ReplicationMode,
        New: ReplicationMode,
        TimeStamp: DUMMY_EPOCH}
)
