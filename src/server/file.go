package momo

import(
    "os"
    "io"
    "log"
    "net"
    "strconv"
    "bytes"

    momo_common "github.com/alsotoes/momo/src/common"
)

func getMetadata(connection net.Conn) momo_common.FileMetadata {
    var metadata momo_common.FileMetadata

    bufferFileMD5 := make([]byte, 32)
    bufferFileName := make([]byte, momo_common.FileInfoLength)
    bufferFileSize := make([]byte, momo_common.FileInfoLength)

    connection.Read(bufferFileMD5)
    fileMD5 := string(bytes.Trim(bufferFileMD5, "\x00"))

    connection.Read(bufferFileName)
    fileName := string(bytes.Trim(bufferFileName, "\x00"))

    connection.Read(bufferFileSize)
    fileSize, _ := strconv.ParseInt(string(bytes.Trim(bufferFileSize, "\x00")), 10, momo_common.FileInfoLength)

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
        if (fileSize - receivedBytes) < momo_common.TCPSocketBufferSize {
            if (fileSize - receivedBytes) != 0 {
                io.CopyN(newFile, connection, (fileSize - receivedBytes))
                connection.Read(make([]byte, (receivedBytes+momo_common.TCPSocketBufferSize)-fileSize))
            }
            break
        }
        io.CopyN(newFile, connection, momo_common.TCPSocketBufferSize)
        receivedBytes += momo_common.TCPSocketBufferSize
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
