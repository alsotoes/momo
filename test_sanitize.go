package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func sanitizeFileName(rawPath string, op string) (string, error) {
	rawName := strings.TrimPrefix(rawPath, "/")
	if rawName == "." || rawName == ".." || strings.Contains(rawName, "/") || strings.Contains(rawName, "\\") {
		return "", &os.PathError{Op: op, Path: rawName, Err: os.ErrInvalid}
	}
	fileName := filepath.Base(rawName)
	if fileName == "." || fileName == ".." || fileName == "/" || fileName == "\\" {
		return "", &os.PathError{Op: op, Path: fileName, Err: os.ErrInvalid}
	}
	return fileName, nil
}

func main() {
	fmt.Println(sanitizeFileName("/../../etc/passwd", "test"))
	fmt.Println(sanitizeFileName("/valid.txt", "test"))
}
