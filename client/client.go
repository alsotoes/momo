package momo

import (
    "os"
    "io"
    "log"
    "net"
    "sync"
    "strconv"

    momo_common "github.com/alsotoes/momo/common"
)

func Connect(wg *sync.WaitGroup, daemons []*momo_common.Daemon, filePath string, serverId int) {
    var connArr [3]net.Conn
    var fileMetadata momo_common.FileMetadata

    file, err := os.Open(filePath)
    if err != nil {
        log.Printf(err.Error())
        os.Exit(1)
    }
    fileInfo, err := file.Stat()
    if err != nil {
        log.Printf(err.Error())
        os.Exit(1)
    }

    hash, err := momo_common.HashFile(filePath)
    if err != nil {
        log.Printf(err.Error())
        os.Exit(1)
    }

    fileMetadata.MD5 = hash
    fileMetadata.Name = fileInfo.Name()
    fileMetadata.Size = fileInfo.Size()

    connArr[0] = dialSocket(daemons[serverId].Host)

    defer wg.Done()
    defer connArr[0].Close()

    bufferReplicationMode := make([]byte, 1)
    connArr[0].Read(bufferReplicationMode)

    if strconv.Itoa(momo_common.PRIMARY_SPLAY_REPLICATION) == string(bufferReplicationMode) {
        log.Printf("Daemon replicationMode: " + string(bufferReplicationMode))
    }

    sendFile(connArr[0], filePath, file, fileMetadata)
}

func sendFile(connection net.Conn, filePath string, file *os.File, fileMetadata momo_common.FileMetadata) {
    fileMD5 := fillString(fileMetadata.MD5, 32)
    fileName := fillString(fileMetadata.Name, momo_common.LENGTHINFO)
    fileSize := fillString(strconv.FormatInt(fileMetadata.Size, 10), momo_common.LENGTHINFO)

    log.Printf("Sending filename and filesize!")
    connection.Write([]byte(fileMD5))
    connection.Write([]byte(fileName))
    connection.Write([]byte(fileSize))
    sendBuffer := make([]byte, momo_common.BUFFERSIZE)

    log.Printf("Start sending file!")
    log.Printf("=> MD5: " + fileMD5)
    log.Printf("=> Name: " + fileName)
    for {
        _, err := file.Read(sendBuffer)
        if err == io.EOF {
            break
        }
        connection.Write(sendBuffer)
    }

    log.Printf("Waiting ACK from server")
    bufferACK := make([]byte, 10)
    connection.Read(bufferACK)
    log.Printf(string(bufferACK))
    log.Printf("File has been sent, closing connection!")
}

func dialSocket(servAddr string) net.Conn {
    tcpAddr, err := net.ResolveTCPAddr("tcp", servAddr)
    if err != nil {
        println("ResolveTCPAddr failed:", err.Error())
        os.Exit(1)
    }

    connection, err := net.DialTCP("tcp", nil, tcpAddr)
    if err != nil {
        println("Dial failed:", err.Error())
        os.Exit(1)
    }

    return connection
}

func fillString(retunString string, toLength int) string {
    for {
        lengtString := len(retunString)
        if lengtString < toLength {
            retunString = retunString + ":"
            continue
        }
        break
    }
    return retunString
}
