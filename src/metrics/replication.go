package momo

import (
    "log"
    "time"
    "encoding/json"

    momo_common "github.com/alsotoes/momo/src/common"
    momo_client "github.com/alsotoes/momo/src/client"
)

func pushNewReplicationMode(replication int) {
    cfg := momo_common.GetConfig()
    conn := momo_client.DialSocket(cfg.Daemons[0].Chrep)
    defer conn.Close()

    now := time.Now()
    nanos := now.UnixNano()

    replicationJsonStruct := &momo_common.ReplicationData{
        New: replication,
        TimeStamp: nanos}

    replicationJson, _ := json.Marshal(replicationJsonStruct)
    conn.Write([]byte(replicationJson))
    log.Printf("New Replication mode pushed: %s", replicationJson)
}
