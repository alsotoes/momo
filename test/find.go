package main

import "fmt"
import "strconv"

func main(){
    a := []string{"2","1","3"}
    x := 1
    fmt.Print(Contains(a,strconv.Itoa(x)))

}

// Contains tells whether a contains x.
func Contains(a []string, x string) int {
        for index, n := range a {
                fmt.Printf("%d,%s,%s\n",index, n ,x)
                if x == n {
                        return index
                }
        }
        return -1
}
