package momo

import(
    "os"
    "log"
    "net"
    "sync"
    "time"
    "strconv"

    momo_common "github.com/alsotoes/momo/common"
    momo_client "github.com/alsotoes/momo/client"
)

func Daemon(daemons []*momo_common.Daemon, serverId int) {
    var timestamp int64
    server, err := net.Listen("tcp", daemons[serverId].Host)
    if err != nil {
        log.Printf("Error listetning: ", err)
        os.Exit(1)
    }

    defer server.Close()
    log.Printf("Server primary Daemon started... at " + daemons[serverId].Host)
    log.Printf("...Waiting for connections...")

    for {
        connection, err := server.Accept()
        if err != nil {
            log.Printf("Error: ", err)
            os.Exit(1)
        }
        log.Printf("Client connected to primary Daemon")

        go func() {
            var replicationMode int
            defer func(){
                log.Printf("Server ACK to Client => ACK"+strconv.Itoa(serverId))
                connection.Write([]byte("ACK"+strconv.Itoa(serverId)))
                connection.Close()
            }()

            bufferTimestamp := make([]byte, momo_common.LENGTHTIMESTAMP)
            connection.Read(bufferTimestamp)
            timestamp, err = strconv.ParseInt(string(bufferTimestamp), 10, 64)
            if err != nil {
                log.Printf("Error: %d of type %T", timestamp, timestamp)
                panic(err)
            }

            if 0 == serverId {
                now := time.Now()
                timestamp = now.UnixNano()
                replicationMode = momo_common.ReplicationLookBack.New
            } else if 1 == serverId {
                if timestamp > momo_common.ReplicationLookBack.TimeStamp {
                    replicationMode = momo_common.ReplicationLookBack.New
                } else {
                    replicationMode = momo_common.ReplicationLookBack.Old
                }

                if replicationMode != momo_common.CHAIN_REPLICATION {
                    replicationMode = momo_common.NO_REPLICATION
                }
            } else {
                replicationMode = momo_common.NO_REPLICATION
            }

            log.Printf("Cluster object global timestamp: " + strconv.FormatInt(timestamp, 10))
            log.Printf("Server Daemon replicationMode: " + strconv.Itoa(replicationMode))
            connection.Write([]byte(strconv.FormatInt(int64(replicationMode), 10)))

            metadata := getMetadata(connection)
            var wg sync.WaitGroup

            switch replicationMode {
                case momo_common.NO_REPLICATION, momo_common.PRIMARY_SPLAY_REPLICATION:
                    getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                case momo_common.CHAIN_REPLICATION:
                    if serverId == 1 {
                        wg.Add(1)
                        momo_client.Connect(&wg, daemons, daemons[1].Data+"/"+metadata.Name, 2, timestamp)
                        wg.Wait()
                    } else {
                        wg.Add(1)
                        getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                        momo_client.Connect(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
                        wg.Wait()
                    }
                case momo_common.SPLAY_REPLICATION:
                    wg.Add(2)
                    getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                    go momo_client.Connect(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1, timestamp)
                    go momo_client.Connect(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 2, timestamp)
                    wg.Wait()
                default:
                    log.Println("*** ERROR: Unknown replication type")
                    os.Exit(1)
            }
        }()
    }
}
