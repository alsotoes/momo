package momo

import (
	"encoding/json"
	"log"
	"net"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

func pushNewReplicationMode(newReplicationMode int) {
	log.Printf("Pushing new replication mode to all daemons")

	config, err := momo_common.GetConfigFromFile()
	if err != nil {
		log.Fatalf("Failed to get config: %v", err)
	}

	for _, daemon := range config.Daemons {
		go func(daemon *momo_common.Daemon) {
			conn, err := net.Dial("unix", daemon.Chrep)
			if err != nil {
				log.Printf("Dial error: %v", err)
				return
			}
			defer conn.Close()

			encoder := json.NewEncoder(conn)
			data := momo_common.ReplicationData{
				New:       newReplicationMode,
				TimeStamp: time.Now().Unix(),
			}

			if err := encoder.Encode(data); err != nil {
				log.Printf("Encode error: %v", err)
			}
		}(daemon)
	}
}
