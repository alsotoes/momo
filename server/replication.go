package momo

import(
    "os"
    "log"
    "net"
    "bytes"
    "strconv"
    "encoding/json"

    momo_common "github.com/alsotoes/momo/common"
    momo_client "github.com/alsotoes/momo/client"
)

func ChangeReplicationModeServer(daemons []*momo_common.Daemon, serverId int, timestamp int64) {
    server, err := net.Listen("tcp", daemons[serverId].Chrep)
    if err != nil {
        log.Printf("Error listetning: ", err)
        os.Exit(1)
    }

    defer server.Close()
    log.Printf("Server changeReplicationMode started... at "+daemons[serverId].Chrep)
    log.Printf("Waiting for connections: changeReplicationMode...")
    log.Printf("default ReplicationMode value: " + strconv.Itoa(momo_common.ReplicationMode))

    momo_common.ReplicationLookBack.TimeStamp = timestamp
    replicationJson, _ := json.Marshal(momo_common.ReplicationLookBack)
    log.Printf("ReplicationData struct: "+ string(replicationJson))

    for {
        connection, err := server.Accept()
        if err != nil {
            log.Printf("Error: ", err)
            os.Exit(1)
        }
        go func() {
            bufferReplicationMode := make([]byte, momo_common.LENGTHINFO)
            connection.Read(bufferReplicationMode)
            log.Printf("Client connected to changeReplicationMode")

            replicationJson := momo_common.ReplicationData{}
            if err := json.NewDecoder(bytes.NewReader(bufferReplicationMode)).Decode(&replicationJson); err != nil {
                panic(err)
            }

            momo_common.ReplicationLookBack.Old = momo_common.ReplicationMode
            momo_common.ReplicationLookBack.New = replicationJson.New
            momo_common.ReplicationLookBack.TimeStamp = replicationJson.TimeStamp
            newReplicationJson, _ := json.Marshal(momo_common.ReplicationLookBack)
            momo_common.ReplicationMode = replicationJson.New
            log.Printf("changeReplicationMode new value: " + strconv.Itoa(replicationJson.New))
            log.Printf("ReplicationData new struct: "+ string(newReplicationJson))

            if 0 == serverId {
                go changeReplicationModeClient(daemons, string(newReplicationJson), 1)
                go changeReplicationModeClient(daemons, string(newReplicationJson), 2)
            }
        }()
    }
}

func changeReplicationModeClient(daemons []*momo_common.Daemon, replicationJson string, serverId int) {
    conn := momo_client.DialSocket(daemons[serverId].Chrep)
    defer conn.Close()

    conn.Write([]byte(replicationJson))
    log.Printf("ReplicationData sent to serverId: "+ strconv.Itoa(serverId))
}
