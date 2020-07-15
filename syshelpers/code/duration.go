package main

import (
    "fmt"
    "time"
)

/*
// https://golang.org/pkg/time/#example_Duration
const (
        Nanosecond  Duration = 1
        Microsecond          = 1000 * Nanosecond
        Millisecond          = 1000 * Microsecond
        Second               = 1000 * Millisecond
        Minute               = 60 * Second
        Hour                 = 60 * Minute
)
*/

func main() {
    fmt.Print(time.Duration(500)*time.Millisecond)
}
