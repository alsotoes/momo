package momo

import(
    "os"
    "log"
    "net"
    "sync"
    "strconv"

    momo_common "github.com/alsotoes/momo/common"
    momo_client "github.com/alsotoes/momo/client"
)

func Daemon(daemons []*momo_common.Daemon, serverId int) {
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

            // TODO: fix this, put this logic in the switch-case code
            if 0 == serverId {
                replicationMode = momo_common.ReplicationMode
            } else {
                replicationMode = momo_common.NO_REPLICATION
            }

            log.Printf("Server Daemon replicationMode: " + strconv.Itoa(replicationMode))
            connection.Write([]byte(strconv.FormatInt(int64(replicationMode), 10)))

            //metadata := momo_server.GetMetadata(connection)
            metadata := GetMetadata(connection)
            var wg sync.WaitGroup

            switch replicationMode {
                case momo_common.NO_REPLICATION, momo_common.PRIMARY_SPLAY_REPLICATION:
                    GetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                    if momo_common.ReplicationMode == momo_common.CHAIN_REPLICATION && 1 == serverId {
                        wg.Add(1)
                        momo_client.Connect(&wg, daemons, daemons[1].Data+"/"+metadata.Name, 2)
                        wg.Wait()
                    }
                case momo_common.CHAIN_REPLICATION:
                    wg.Add(1)
                    GetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                    momo_client.Connect(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1)
                    wg.Wait()
                case momo_common.SPLAY_REPLICATION:
                    wg.Add(2)
                    GetFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                    go momo_client.Connect(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1)
                    go momo_client.Connect(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 2)
                    wg.Wait()
                default:
                    log.Println("*** ERROR: Unknown replication type")
                    os.Exit(1)
            }
        }()
    }
}
