package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("http://localhost/%2e%2e/%2e%2e/etc/passwd")
	fmt.Println("Path:", u.Path)
	fmt.Println("RawPath:", u.RawPath)
	fmt.Println("EscapedPath:", u.EscapedPath())
}
