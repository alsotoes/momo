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

type FileMetadata struct {
    name string
    md5  string
    size int64
}

func Daemon(ip string, port int, path string, replicationType int) {
    servAddr := ip + ":" + strconv.Itoa(port)
    server, err := net.Listen("tcp", servAddr)
    if err != nil {
        log.Printf("Error listetning: ", err)
        os.Exit(1)
    }

    defer server.Close()
    log.Printf("Server primary Daemon started... waiting for connections...")

    for {
        connection, err := server.Accept()
        if err != nil {
            log.Printf("Error: ", err)
            os.Exit(1)
        }
        log.Printf("Client connected to primary Daemon")

        go func() {

            log.Printf("Primary Daemon replicationMode: " + strconv.Itoa(momo_common.ReplicationMode))
            connection.Write([]byte(strconv.FormatInt(int64(momo_common.ReplicationMode), 10)))

            metadata := getMetadata(connection)
            var wg sync.WaitGroup

            switch replicationType {
                case 0:
                    getFile(connection, path, metadata.name, metadata.md5, metadata.size)
                case 1:
                    getFile(connection, path, metadata.name, metadata.md5, metadata.size)
                    wg.Add(2)
                    momo_client.Connect(&wg, "0.0.0.0", 3334, "./received_files/dir1/"+metadata.name)
                    momo_client.Connect(&wg, "0.0.0.0", 3335, "./received_files/dir1/"+metadata.name)
                case 2:
                    getFile(connection, path, metadata.name, metadata.md5, metadata.size)
                    wg.Add(2)
                    go momo_client.Connect(&wg, "0.0.0.0", 3334, "./received_files/dir1/"+metadata.name)
                    go momo_client.Connect(&wg, "0.0.0.0", 3335, "./received_files/dir1/"+metadata.name)
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
    log.Printf("Server changeReplicationMode started... waiting for connections...")
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

func getMetadata(connection net.Conn) FileMetadata {
    var metadata FileMetadata

    bufferFileMD5 := make([]byte, 32)
    bufferFileName := make([]byte, momo_common.LENGTHINFO)
    bufferFileSize := make([]byte, momo_common.LENGTHINFO)

    connection.Read(bufferFileMD5)
    fileMD5 := string(bufferFileMD5)

    connection.Read(bufferFileName)
    fileName := strings.Trim(string(bufferFileName), ":")

    connection.Read(bufferFileSize)
    fileSize, _ := strconv.ParseInt(strings.Trim(string(bufferFileSize), ":"), 10, momo_common.LENGTHINFO)

    metadata.name = fileName
    metadata.md5 = fileMD5
    metadata.size = fileSize

    return metadata

}

func getFile(connection net.Conn, path string, fileName string, fileMD5 string, fileSize int64) {

    newFile, err := os.Create(path+fileName)

    if err != nil {
        panic(err)
    }

    defer connection.Close()
    defer newFile.Close()
    var receivedBytes int64

    for {
        if (fileSize - receivedBytes) < momo_common.BUFFERSIZE {
            io.CopyN(newFile, connection, (fileSize - receivedBytes))
            connection.Read(make([]byte, (receivedBytes+momo_common.BUFFERSIZE)-fileSize))
            break
        }
        io.CopyN(newFile, connection, momo_common.BUFFERSIZE)
        receivedBytes += momo_common.BUFFERSIZE
    }

    hash, err := momo_common.HashFile(path+fileName)
    if err != nil {
        log.Printf(err.Error())
        os.Exit(1)
    }

    connection.Write([]byte("ACK"))

    log.Printf("=> MD5:     " + fileMD5)
    log.Printf("=> New MD5: " + hash)
    log.Printf("=> Name:    " + path + fileName)
    log.Printf("Received file completely!")

}
