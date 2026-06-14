package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("http://localhost/../../etc/passwd")
	fmt.Println("Path:", u.Path)

	u2, _ := url.Parse("http://localhost/%2e%2e/%2e%2e/etc/passwd")
	fmt.Println("Path:", u2.Path)
}
