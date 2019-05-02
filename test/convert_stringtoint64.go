package main

import (
    "fmt"
    "time"
    "strconv"
)

func main() {

    now := time.Now()
    nanos := now.UnixNano()
    bufferTimestamp := strconv.FormatInt(nanos, 10)

    fmt.Printf("bufferTimestamp value: %s\n", bufferTimestamp)
    timestamp, err := strconv.ParseInt(string(bufferTimestamp), 10, 64)
    if err != nil {
        fmt.Printf("Error: %d of type %T\n", timestamp, timestamp)
        panic(err)
    } else {
        fmt.Printf("Converted value: %d\n", timestamp)
    }

}
