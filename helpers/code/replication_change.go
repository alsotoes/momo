package main

import (
    "time"
    "encoding/json"

    momo_client "github.com/alsotoes/momo/src/client"
    momo_common "github.com/alsotoes/momo/src/common"
)

func main() {
    cfg := momo_common.GetConfig()
    conn := momo_client.DialSocket(cfg.Daemons[0].Chrep)
    defer conn.Close()

    now := time.Now()
    nanos := now.UnixNano()

    replicationJsonStruct := &momo_common.ReplicationData{
        Old: 2,
        New: 1,
        TimeStamp: nanos}

    replicationJson, _ := json.Marshal(replicationJsonStruct)
    conn.Write([]byte(replicationJson))
}
