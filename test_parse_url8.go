package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("http://localhost/../../etc/passwd")
	fmt.Println("Path:", u.Path)

	u2, _ := url.Parse("http://localhost/%2E%2E/%2E%2E/etc/passwd")
	fmt.Println("Path:", u2.Path)
}
