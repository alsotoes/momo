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
    connArr[0] = dialSocket(daemons[serverId].Host)

    defer wg.Done()
    defer connArr[0].Close()

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

    bufferReplicationMode := make([]byte, 1)
    connArr[0].Read(bufferReplicationMode)

    if strconv.Itoa(momo_common.PRIMARY_SPLAY_REPLICATION) == string(bufferReplicationMode) {
        log.Printf(string(bufferReplicationMode))
    }

    fileMD5 := fillString(hash, 32)
    fileName := fillString(fileInfo.Name(), momo_common.LENGTHINFO)
    fileSize := fillString(strconv.FormatInt(fileInfo.Size(), 10), momo_common.LENGTHINFO)

    log.Printf("Sending filename and filesize!")
    connArr[0].Write([]byte(fileMD5))
    connArr[0].Write([]byte(fileName))
    connArr[0].Write([]byte(fileSize))
    sendBuffer := make([]byte, momo_common.BUFFERSIZE)

    log.Printf("Start sending file!")
    log.Printf("=> MD5: " + fileMD5)
    log.Printf("=> Name: " + fileName)
    for {
        _, err = file.Read(sendBuffer)
        if err == io.EOF {
            break
        }
        connArr[0].Write(sendBuffer)
    }

    log.Printf("Waiting ACK from server")
    bufferACK := make([]byte, 10)
    connArr[0].Read(bufferACK)
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
