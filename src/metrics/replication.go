package momo

import (
	"encoding/json"
	"log"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

func pushNewReplicationMode(newReplicationMode int) {
	log.Printf("Pushing new replication mode to all daemons")

	config, err := momo_common.GetConfigFromFile()
	if err != nil {
		log.Fatalf("Failed to get config: %v", err)
	}

    conn, err := momo_common.DialSocket(config.Daemons[0].ChangeReplication)
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
}
