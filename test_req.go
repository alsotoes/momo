package main

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
)

func main() {
	reqStr := "PUT /../../etc/passwd HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"
	req, _ := http.ReadRequest(bufio.NewReader(strings.NewReader(reqStr)))
	fmt.Printf("Path: %s\n", req.URL.Path)
}
