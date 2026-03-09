package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const TCPSocketBufferSize = 1024

func sendLoop(conn net.Conn, file *os.File) {
	sendBuffer := make([]byte, TCPSocketBufferSize)
	for {
		n, err := file.Read(sendBuffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		conn.Write(sendBuffer[:n])
	}
}

func sendCopy(conn net.Conn, file *os.File) {
	io.Copy(conn, file)
}

func receiveLoop(conn net.Conn, file *os.File, fileSize int64) {
	var receivedBytes int64
	for {
		if (fileSize - receivedBytes) < TCPSocketBufferSize {
			if (fileSize - receivedBytes) != 0 {
				io.CopyN(file, conn, (fileSize - receivedBytes))
			}
			break
		}
		io.CopyN(file, conn, TCPSocketBufferSize)
		receivedBytes += TCPSocketBufferSize
	}
}

func receiveCopyN(conn net.Conn, file *os.File, fileSize int64) {
	if fileSize > 0 {
		io.CopyN(file, conn, fileSize)
	}
}

func main() {
	// Create a large file
	fileName := "largefile.dat"
	f, _ := os.Create(fileName)
	f.Truncate(100 * 1024 * 1024) // 100 MB
	f.Close()

	fileSize := int64(100 * 1024 * 1024)

	// test loop
	ln, _ := net.Listen("tcp", ":0")
	go func() {
		conn, _ := ln.Accept()
		file, _ := os.Open(fileName)
		sendLoop(conn, file)
		conn.Close()
		file.Close()
	}()
	conn, _ := net.Dial("tcp", ln.Addr().String())
	outFile, _ := os.Create("out1.dat")
	start := time.Now()
	receiveLoop(conn, outFile, fileSize)
	fmt.Printf("Loop time: %v\n", time.Since(start))
	outFile.Close()
	conn.Close()

	// test copy
	ln2, _ := net.Listen("tcp", ":0")
	go func() {
		conn, _ := ln2.Accept()
		file, _ := os.Open(fileName)
		sendCopy(conn, file)
		conn.Close()
		file.Close()
	}()
	conn2, _ := net.Dial("tcp", ln2.Addr().String())
	outFile2, _ := os.Create("out2.dat")
	start = time.Now()
	receiveCopyN(conn2, outFile2, fileSize)
	fmt.Printf("Copy time: %v\n", time.Since(start))
	outFile2.Close()
	conn2.Close()
}
