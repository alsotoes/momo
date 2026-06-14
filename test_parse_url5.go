package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("http://localhost/%2E%2E/%2E%2E/etc/passwd")
	fmt.Println("Path:", u.Path)
	fmt.Println("RawPath:", u.RawPath)
}
