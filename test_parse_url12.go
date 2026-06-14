package main

import (
	"fmt"
	"net/http"
	"strings"
	"bufio"
	"path/filepath"
)

func parsePath(reqPath string) (string, error) {
	rawName := strings.TrimPrefix(reqPath, "/")
	if rawName == "." || rawName == ".." || strings.Contains(rawName, "/") || strings.Contains(rawName, "\\") {
		return "", fmt.Errorf("invalid path: %s", rawName)
	}
	fileName := filepath.Base(rawName)
	if fileName == "." || fileName == ".." || fileName == "/" || fileName == "\\" {
		return "", fmt.Errorf("invalid filename: %s", fileName)
	}
	return fileName, nil
}

func main() {
	reqStr := "PUT /../etc/passwd HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"
	req, _ := http.ReadRequest(bufio.NewReader(strings.NewReader(reqStr)))
	fmt.Println("URL Path:", req.URL.Path)

	res, err := parsePath(req.URL.Path)
	fmt.Println("Result:", res, "Err:", err)
}
