package main

import (
	"fmt"
	"path"
)

func main() {
	path1 := path.Clean("/../../etc/passwd")
	fmt.Println("Path 1:", path1)

	path2 := path.Base("/../../etc/passwd")
	fmt.Println("Path 2:", path2)
}
