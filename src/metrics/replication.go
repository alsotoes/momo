// Package metrics provides the metrics collection and analysis functionality for the momo application.
package metrics

import (
	"encoding/json"
	"log"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

// pushNewReplicationMode notifies the primary daemon of a replication mode change.
// It connects to the ChangeReplication endpoint of the first daemon listed in the configuration
// and sends a JSON payload containing the new replication mode and the current timestamp.
func pushNewReplicationMode(newReplicationMode int) {
	log.Printf("Notifying primary daemon of new replication mode: %d", newReplicationMode)

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
