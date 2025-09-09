package main

import (
    "fmt"
    "time"
)

func main() {
    now := time.Now()
    nanos := now.UnixNano()
    fmt.Print(nanos) 
}
