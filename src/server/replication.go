package momo

import(
    "os"
    "log"
    "net"
    "bytes"
    "encoding/json"

    momo_common "github.com/alsotoes/momo/src/common"
)

var CurrentReplicationMode int = momo_common.ReplicationNone
var ReplicationState momo_common.ReplicationData

func ChangeReplicationModeServer(daemons []*momo_common.Daemon, serverId int, timestamp int64) {
    server, err := net.Listen("tcp", daemons[serverId].ChangeReplication)
    if err != nil {
        log.Printf("Error listetning: %v", err)
        os.Exit(1)
    }

    defer server.Close()
    log.Printf("Server changeReplicationMode started... at %s", daemons[serverId].ChangeReplication)
    log.Printf("Waiting for connections: changeReplicationMode...")
    log.Printf("default ReplicationMode value: %d", CurrentReplicationMode)

    ReplicationState.Old = CurrentReplicationMode
    ReplicationState.New = CurrentReplicationMode
    ReplicationState.TimeStamp = timestamp
    replicationJson, _ := json.Marshal(ReplicationState)
    log.Printf("ReplicationData struct: %s", string(replicationJson))

    for {
        connection, err := server.Accept()
        if err != nil {
            log.Printf("Error: %v", err)
            os.Exit(1)
        }
        go func() {
            bufferReplicationMode := make([]byte, momo_common.FileInfoLength)
            connection.Read(bufferReplicationMode)
            log.Printf("Client connected to changeReplicationMode")

            replicationJson := momo_common.ReplicationData{}
            if err := json.NewDecoder(bytes.NewReader(bufferReplicationMode)).Decode(&replicationJson); err != nil {
                panic(err)
            }

            ReplicationState.Old = CurrentReplicationMode
            ReplicationState.New = replicationJson.New
            ReplicationState.TimeStamp = replicationJson.TimeStamp
            newReplicationJson, _ := json.Marshal(ReplicationState)
            CurrentReplicationMode = replicationJson.New
            log.Printf("changeReplicationMode new value: %d", replicationJson.New)
            log.Printf("ReplicationData new struct: %s", string(newReplicationJson))

            if 0 == serverId {
                go changeReplicationModeClient(daemons, string(newReplicationJson), 1)
                go changeReplicationModeClient(daemons, string(newReplicationJson), 2)
            }
        }()
    }
}

func changeReplicationModeClient(daemons []*momo_common.Daemon, replicationJson string, serverId int) {
    conn, _ := momo_common.DialSocket(daemons[serverId].ChangeReplication)
    defer conn.Close()

    conn.Write([]byte(replicationJson))
    log.Printf("ReplicationData sent to serverId: %d", serverId)
}
