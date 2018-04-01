package momo

import (
	"log"
	"net"
	"os"
    "io"
	"strconv"

    momo_common "github.com/alsotoes/momo/common"
)

const BUFFERSIZE = 1024
const LENGTHINFO = 64

func Connect(ip string, port int, filePath string) {

    servAddr := ip + ":" + strconv.Itoa(port)
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

    hash, err := momo_common.HashFile_md5(filePath)
    if err != nil {
        log.Printf(err.Error())
        os.Exit(1)
    }

	fileMD5 := fillString(hash, LENGTHINFO)
	fileName := fillString(fileInfo.Name(), LENGTHINFO)

	log.Printf("Sending filename and filesize!")
	connection.Write([]byte(fileMD5))
	connection.Write([]byte(fileName))
	sendBuffer := make([]byte, BUFFERSIZE)

	log.Printf("Start sending file!")
	for {
		_, err = file.Read(sendBuffer)
		if err == io.EOF {
			break
		}
		connection.Write(sendBuffer)
	}
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
