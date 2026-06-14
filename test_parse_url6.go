package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

func parsePath(reqPath string) (string, error) {
	rawName := strings.TrimPrefix(reqPath, "/")
	if rawName == "." || rawName == ".." || strings.Contains(rawName, "/") || strings.Contains(rawName, "\\") {
		return "", fmt.Errorf("invalid path: %s", rawName)
	}
	fileName := filepath.Base(rawName)
	if fileName == "." || fileName == ".." || fileName == "/" || fileName == "\\" {
		return "", fmt.Errorf("invalid path: %s", fileName)
	}
	return fileName, nil
}

func main() {
	u, _ := url.Parse("http://localhost/../../etc/passwd")
	fmt.Println("Path:", u.Path)
	res, err := parsePath(u.Path)
	fmt.Println("Result:", res, "Err:", err)
}
