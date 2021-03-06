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

func sendFile(wgSendFile *sync.WaitGroup, connection net.Conn, filePath string) {
    defer wgSendFile.Done()

    file, err := os.Open(filePath)
    if err != nil {
        log.Printf(err.Error())
        os.Exit(1)
    }else {
        defer file.Close()
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
    log.Printf("=> Name: " + fileInfo.Name())
    for {
        _, err := file.Read(sendBuffer)
        if err == io.EOF {
            break
        }
        connection.Write(sendBuffer)
    }

    log.Printf("Waiting ACK from server")
    bufferACK := make([]byte, 4)
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
