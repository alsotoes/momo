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
	// ⚡ Bolt: Avoid json.NewEncoder(conn) to prevent un-consolidated network writes.
	// Serialize first, then combine with AuthToken to send in a single write operation.
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Marshal error: %v", err)
		return
	}

	// ⚡ Bolt: Combine AuthToken and JSON payload into a single network write using a stack-allocated buffer
	// to reduce system calls and eliminate heap allocations. json.Encoder adds a newline, so we append it here too.
	var payloadBuf [1024]byte
	payload := append(payloadBuf[:0], paddedAuthToken...)
	payload = append(payload, jsonData...)
	payload = append(payload, '\n')

	if _, err := conn.Write(payload); err != nil {
		log.Printf("Failed to send replication data: %v", err)
	}
}
