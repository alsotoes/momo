package main

import (
	"fmt"
	"net/url"
)

func main() {
	path := "test/../../etc/passwd"
	urlStr := fmt.Sprintf("http://127.0.0.1/%s", url.PathEscape(path))
	u, _ := url.Parse(urlStr)
	fmt.Println("Path:", u.Path)
}
