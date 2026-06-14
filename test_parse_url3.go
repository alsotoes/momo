package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("http://localhost/../../etc/passwd")
	fmt.Println("Path:", u.Path)
}
