package momo

import(
    "os"
    "io"
    "log"
    "net"
    "sync"
    _ "bytes"
    _ "bufio"
    "strconv"
    "strings"
    _ "io/ioutil"
    _ "reflect"

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

            metadata := getMetadata(connection)
            var wg sync.WaitGroup

            switch replicationMode {
                case momo_common.NO_REPLICATION, momo_common.PRIMARY_SPLAY_REPLICATION:
                    getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                    if momo_common.ReplicationMode == momo_common.CHAIN_REPLICATION && 1 == serverId {
                        wg.Add(1)
                        momo_client.Connect(&wg, daemons, daemons[1].Data+"/"+metadata.Name, 2)
                        wg.Wait()
                    }
                case momo_common.CHAIN_REPLICATION:
                    wg.Add(1)
                    getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
                    momo_client.Connect(&wg, daemons, daemons[0].Data+"/"+metadata.Name, 1)
                    wg.Wait()
                case momo_common.SPLAY_REPLICATION:
                    wg.Add(2)
                    getFile(connection, daemons[serverId].Data+"/", metadata.Name, metadata.MD5, metadata.Size)
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

func ChangeReplicationMode(servAddr string) {
    server, err := net.Listen("tcp", servAddr)
    if err != nil {
        log.Printf("Error listetning: ", err)
        os.Exit(1)
    }

    defer server.Close()
    log.Printf("Server changeReplicationMode started... at "+servAddr)
    log.Printf("...waiting for connections...")
    log.Printf("default ReplicationMode value: " + strconv.Itoa(momo_common.ReplicationMode))

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
            log.Printf("go ChangeReplicationMode new value: " + strconv.Itoa(momo_common.ReplicationMode))
        }()
    }
}

func getMetadata(connection net.Conn) momo_common.FileMetadata {
    var metadata momo_common.FileMetadata

    bufferFileMD5 := make([]byte, 32)
    bufferFileName := make([]byte, momo_common.LENGTHINFO)
    bufferFileSize := make([]byte, momo_common.LENGTHINFO)

    connection.Read(bufferFileMD5)
    fileMD5 := string(bufferFileMD5)

    connection.Read(bufferFileName)
    fileName := strings.Trim(string(bufferFileName), ":")

    connection.Read(bufferFileSize)
    fileSize, _ := strconv.ParseInt(strings.Trim(string(bufferFileSize), ":"), 10, momo_common.LENGTHINFO)

    metadata.Name = fileName
    metadata.MD5 = fileMD5
    metadata.Size = fileSize

    return metadata

}

func getFile(connection net.Conn, path string, fileName string, fileMD5 string, fileSize int64) {

    newFile, err := os.Create(path+fileName)

    if err != nil {
        log.Printf(err.Error())
        connection.Close()
        os.Exit(1)
    }

    // TODO: Check on error inside this procedure for posible exit with the connection open :(
    // https://stackoverflow.com/questions/12741386/how-to-know-tcp-connection-is-closed-in-golang-net-package
    // defer connection.Close()
    defer newFile.Close()
    var receivedBytes int64

    for {
        if (fileSize - receivedBytes) < momo_common.BUFFERSIZE {
            if (fileSize - receivedBytes) != 0 {
                io.CopyN(newFile, connection, (fileSize - receivedBytes))
                connection.Read(make([]byte, (receivedBytes+momo_common.BUFFERSIZE)-fileSize))
            }
            break
        }
        io.CopyN(newFile, connection, momo_common.BUFFERSIZE)
        receivedBytes += momo_common.BUFFERSIZE
    }

    hash, err := momo_common.HashFile(path+fileName)
    if err != nil {
        log.Printf(err.Error())
        connection.Close()
        os.Exit(1)
    }

    log.Printf("=> MD5:     " + fileMD5)
    log.Printf("=> New MD5: " + hash)
    log.Printf("=> Name:    " + path + fileName)
    log.Printf("Received file completely!")
    log.Printf("Sending ACK to client connection")
}
