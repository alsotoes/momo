package momo

import(
    "os"
    "log"
    "net"
    "time"
    "strconv"
    "encoding/json"

    momo_common "github.com/alsotoes/momo/common"
)

func ChangeReplicationMode(servAddr string) {
    server, err := net.Listen("tcp", servAddr)
    if err != nil {
        log.Printf("Error listetning: ", err)
        os.Exit(1)
    }

    defer server.Close()
    log.Printf("Server changeReplicationMode started... at "+servAddr)
    log.Printf("Waiting for connections: changeReplicationMode...")
    log.Printf("default ReplicationMode value: " + strconv.Itoa(momo_common.ReplicationMode))

    now := time.Now()
    nanos := now.UnixNano()
    replication := &momo_common.ReplicationData{
        Old: momo_common.ReplicationMode,
        New: momo_common.ReplicationMode,
        TimeStamp: nanos}
    replicationJson, _ := json.Marshal(replication)
    log.Printf("ReplicationData struct: "+ string(replicationJson))

    for {
        connection, err := server.Accept()
        if err != nil {
            log.Printf("Error: ", err)
            os.Exit(1)
        }
        log.Printf("Client connected to changeReplicationMode")
        go func() {
            bufferReplicationMode := make([]byte, 1)
            connection.Read(bufferReplicationMode)
            momo_common.ReplicationMode, _ = strconv.Atoi(string(bufferReplicationMode))
            log.Printf("changeReplicationMode new value: " + strconv.Itoa(momo_common.ReplicationMode))
        }()
    }
}
