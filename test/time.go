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
    now1 := time.Now()
    now2:= time.Now()
    fmt.Printf("%s\n",now2.Sub(now1).String())
    fmt.Printf("%s\n",(time.Duration(5) * time.Millisecond).String())



    if time.Duration(now2.Sub(now1)) < (time.Duration(5) * time.Millisecond) {
        fmt.Print("*** menos")
    }else {
        fmt.Print("*** mas")
    }
}
