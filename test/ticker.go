package main

import (
  	"time"
	"log"
	"net/http"
  	_ "net/http/pprof"  
)

func main() {
	go func() {  
		log.Println(http.ListenAndServe("momo-server0:6060", nil))  
	}()

	ticker := time.NewTicker(time.Millisecond)
  	defer ticker.Stop()
  	for {
    		select {
    			case <-ticker.C:
    		}
  	}
}
