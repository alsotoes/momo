package main

import (
	"fmt"
	"net/http"
	"strings"
	"bufio"
)

func main() {
	reqStr := "PUT /../etc/passwd HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"
	req, _ := http.ReadRequest(bufio.NewReader(strings.NewReader(reqStr)))
	fmt.Println("URL Path:", req.URL.Path)

	reqStr2 := "PUT /%2E%2E/%2E%2E/etc/passwd HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"
	req2, _ := http.ReadRequest(bufio.NewReader(strings.NewReader(reqStr2)))
	fmt.Println("URL Path2:", req2.URL.Path)
}
