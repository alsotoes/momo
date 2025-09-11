package momo

import(
    "os"
    "log"
    "net"
    "sync"
    "time"
    "strconv"

    momo_common "github.com/alsotoes/momo/src/common"
)

var connectToPeer = momo_common.Connect

func Daemon(daemons []*momo_common.Daemon, serverId int) {
    var timestamp int64
    server, err := net.Listen("tcp", daemons[serverId].Host)
    if err != nil {
        log.Printf("Error listetning: %v", err)
        os.Exit(1)
    }

    defer server.Close()
    log.Printf("Server primary Daemon started... at %s", daemons[serverId].Host)
    log.Printf("...Waiting for connections...")

    for {
        connection, err := server.Accept()
        if err != nil {
            log.Printf("Error: %v", err)
            os.Exit(1)
        }
        log.Printf("Client connected to primary Daemon")

        go func() {
            var replicationMode int
            defer func(){
                log.Printf("Server ACK to Client => ACK%d", serverId)
                connection.Write([]byte("ACK"+strconv.Itoa(serverId)))
                connection.Close()
            }()

            bufferTimestamp := make([]byte, momo_common.TimestampLength)
            connection.Read(bufferTimestamp)
            timestamp, err = strconv.ParseInt(string(bufferTimestamp), 10, 64)
            if err != nil {
                log.Printf("Error: %d of type %T", timestamp, timestamp)
                panic(err)
            }

            if 0 == serverId {
                now := time.Now()
                timestamp = now.UnixNano()
                replicationMode = ReplicationState.New
            } else if 1 == serverId {
                if timestamp > ReplicationState.TimeStamp {
                    replicationMode = ReplicationState.New
                } else {
                    replicationMode = ReplicationState.Old
                }

                if replicationMode != momo_common.ReplicationChain {
                    replicationMode = momo_common.ReplicationNone
                }
            } else {
                replicationMode = momo_common.ReplicationNone
            }

            log.Printf("Cluster object global timestamp: %d", timestamp)
            log.Printf("Server Daemon replicationMode: %d", replicationMode)
            connection.Write([]byte(strconv.FormatInt(int64(replicationMode), 10)))

            metadata := getMetadata(connection)
            var wg sync.WaitGroup

            switch replicationMode {
                case momo_common.ReplicationNone, momo_common.ReplicationPrimarySplay:
                    getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                case momo_common.ReplicationChain:
                    if serverId == 1 {
                        wg.Add(1)
                        getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                        connectToPeer(&wg, daemons, daemons[1].Data+"/"+metadata.Name, 2, timestamp)
                        wg.Wait()
                    } else {
                        wg.Add(1)
                        getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                        connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
                        wg.Wait()
                    }
                case momo_common.ReplicationSplay:
                    wg.Add(2)
                    getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                    go connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
                    go connectToPeer(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 2, timestamp)
                    wg.Wait()
                default:
                    log.Println("*** ERROR: Unknown replication type")
                    os.Exit(1)
            }
        }()
    }
}
