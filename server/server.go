package momo

import(
    "strconv"
    "strings"
    "log"
    "net"
    "os"
    "io"

    momo_common "github.com/alsotoes/momo/common"
)

const BUFFERSIZE = 1024
const LENGTHINFO = 64

func Daemon(ip string, port int) {
    servAddr := ip + ":" + strconv.Itoa(port)
    server, err := net.Listen("tcp", servAddr)
	if err != nil {
		log.Printf("Error listetning: ", err)
		os.Exit(1)
	}

	defer server.Close()
	log.Printf("Server started... waiting for connections...")

	for {
		connection, err := server.Accept()
		if err != nil {
			log.Printf("Error: ", err)
			os.Exit(1)
		}
		log.Printf("Client connected")
		go getFile(connection)
	}
}

func getFile(connection net.Conn) {
	bufferFileMD5 := make([]byte, 32)
	bufferFileName := make([]byte, LENGTHINFO)
	bufferFileSize := make([]byte, LENGTHINFO)

	connection.Read(bufferFileMD5)
	fileMD5 := string(bufferFileMD5)

	connection.Read(bufferFileName)
	fileName := strings.Trim(string(bufferFileName), ":")

    connection.Read(bufferFileSize)
	fileSize, _ := strconv.ParseInt(strings.Trim(string(bufferFileSize), ":"), 10, LENGTHINFO)

    /* 
        FIXME: Path can not be hardcoded
        TRELLO CARD: https://trello.com/c/seWPJXSn
        GITHUB ISSUE: https://github.com/alsotoes/momo/issues/1
    */
	newFile, err := os.Create("./received_files/"+fileName)

	if err != nil {
		panic(err)
	}
	defer newFile.Close()
	var receivedBytes int64

	for {
		if (fileSize - receivedBytes) < BUFFERSIZE {
			io.CopyN(newFile, connection, (fileSize - receivedBytes))
			connection.Read(make([]byte, (receivedBytes+BUFFERSIZE)-fileSize))
			break
		}
		io.CopyN(newFile, connection, BUFFERSIZE)
		receivedBytes += BUFFERSIZE
	}

    /*
        FIXME: Path can not be hardcoded
        TRELLO CARD: https://trello.com/c/WihEmAVs
        GITHUB ISSUE: https://github.com/alsotoes/momo/issues/2
    */
    hash, err := momo_common.HashFile_md5("./received_files/"+fileName)
    if err != nil {
        log.Printf(err.Error())
        os.Exit(1)
    }

    log.Printf("=> MD5:     " + fileMD5)
    log.Printf("=> New MD5: " + hash)
    log.Printf("=> Name:    " + fileName)
	log.Printf("Received file completely!")
}
