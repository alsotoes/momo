package common

// Socket constants
const BUFFERSIZE = 1024
const LENGTHINFO = 64

// Replication types
const NO_REPLICATION = 0
const CHAIN_REPLICATION = 1
const SPLAY_REPLICATION = 2
const PRIMARY_SPLAY_REPLICATION = 3

// ReplicationMode at stating point
var ReplicationMode = 1

// Struct to lookback states when changing replication mode
var ReplicationLookBack = &ReplicationData{
    Old: ReplicationMode,
    New: ReplicationMode,
    TimeStamp: 1}
