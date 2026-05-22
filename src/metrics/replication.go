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
// and sends the AuthToken followed by a JSON payload containing the new replication mode and the current timestamp.
func pushNewReplicationMode(cfg momo_common.Configuration, paddedAuthToken []byte, newReplicationMode int) {
	log.Printf("Notifying primary daemon of new replication mode: %d", newReplicationMode)

	conn, err := momo_common.DialSocket(cfg.Daemons[0].ChangeReplication)
	if err != nil {
		log.Printf("Dial error: %v", err)
		return
	}
	defer conn.Close()

	data := momo_common.ReplicationData{
		New:       newReplicationMode,
		TimeStamp: time.Now().UnixNano(),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Marshal error: %v", err)
		return
	}

	// ⚡ Bolt: Consolidate network writes to avoid multiple system calls and network delays.
	// Allocate a single dynamic slice sized for the payload + newline.
	payload := make([]byte, 0, len(paddedAuthToken)+len(jsonData)+1)
	payload = append(payload, paddedAuthToken...)
	payload = append(payload, jsonData...)
	payload = append(payload, '\n') // Required for decoder compatibility

	if _, err := conn.Write(payload); err != nil {
		log.Printf("Failed to send replication data: %v", err)
	}
}
