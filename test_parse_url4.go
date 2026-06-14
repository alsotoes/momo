package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

func sanitizeFileName(rawPath string, op string) (string, error) {
	rawName := strings.TrimPrefix(rawPath, "/")
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
	u, _ := url.Parse("http://localhost/../../etc/passwd")
	fmt.Println("Path:", u.Path)
	res, err := sanitizeFileName(u.Path, "test")
	fmt.Println("Result:", res, "Err:", err)
}
