package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("/../etc/passwd")
	fmt.Println("Path:", u.Path)
}
