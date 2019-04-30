package momo

import (
    "os"
    "log"
    "net"
    "sync"
    "strconv"

    momo_common "github.com/alsotoes/momo/common"
)

func Connect(wg *sync.WaitGroup, daemons []*momo_common.Daemon, filePath string, serverId int) {
    var connArr [3]net.Conn
    var wgSendFile sync.WaitGroup

    connArr[0] = DialSocket(daemons[serverId].Host)

    defer wg.Done()
    defer connArr[0].Close()

    bufferReplicationMode := make([]byte, 1)
    connArr[0].Read(bufferReplicationMode)

    if strconv.Itoa(momo_common.PRIMARY_SPLAY_REPLICATION) == string(bufferReplicationMode) {
        log.Printf("Daemon replicationMode: " + string(bufferReplicationMode))

        connArr[1] = DialSocket(daemons[1].Host)
        defer connArr[1].Close()
        bufferReplicationMode1 := make([]byte, 1)
        connArr[1].Read(bufferReplicationMode1)

        connArr[2] = DialSocket(daemons[2].Host)
        defer connArr[2].Close()
        bufferReplicationMode2 := make([]byte, 1)
        connArr[2].Read(bufferReplicationMode2)

        wgSendFile.Add(3)
        go sendFile(&wgSendFile, connArr[0], filePath)
        go sendFile(&wgSendFile, connArr[1], filePath)
        go sendFile(&wgSendFile, connArr[2], filePath)
    } else {
        wgSendFile.Add(1)
        sendFile(&wgSendFile, connArr[0], filePath)
    }
    wgSendFile.Wait()
}

func DialSocket(servAddr string) net.Conn {
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
