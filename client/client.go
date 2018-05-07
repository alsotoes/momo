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

func Connect(wg *sync.WaitGroup, servAddr string, filePath string) {

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

    defer wg.Done()
    defer connection.Close()

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
    connection.Read(bufferReplicationMode)

    if strconv.Itoa(momo_common.PRIMARY_SPLAY_REPLICATION) == string(bufferReplicationMode) {
        log.Printf(string(bufferReplicationMode))
    }

    fileMD5 := fillString(hash, 32)
    fileName := fillString(fileInfo.Name(), momo_common.LENGTHINFO)
    fileSize := fillString(strconv.FormatInt(fileInfo.Size(), 10), momo_common.LENGTHINFO)

    log.Printf("Sending filename and filesize!")
    connection.Write([]byte(fileMD5))
    connection.Write([]byte(fileName))
    connection.Write([]byte(fileSize))
    sendBuffer := make([]byte, momo_common.BUFFERSIZE)

    log.Printf("Start sending file!")
    log.Printf("=> MD5: " + fileMD5)
    log.Printf("=> Name: " + fileName)
    for {
        _, err = file.Read(sendBuffer)
        if err == io.EOF {
            break
        }
        connection.Write(sendBuffer)
    }

    bufferACK := make([]byte, 10)
    connection.Read(bufferACK)
    log.Printf(string(bufferACK))
    log.Printf("File has been sent, closing connection!")

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
