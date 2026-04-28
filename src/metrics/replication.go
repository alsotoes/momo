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
func pushNewReplicationMode(cfg momo_common.Configuration, newReplicationMode int) {
	log.Printf("Notifying primary daemon of new replication mode: %d", newReplicationMode)

	conn, err := momo_common.DialSocket(cfg.Daemons[0].ChangeReplication)
	if err != nil {
		log.Printf("Dial error: %v", err)
		return
	}
	defer conn.Close()

	// Send the AuthToken first
	if _, err := conn.Write([]byte(momo_common.PadString(cfg.Global.AuthToken, momo_common.AuthTokenLength))); err != nil {
		log.Printf("Failed to send AuthToken: %v", err)
		return
	}

	encoder := json.NewEncoder(conn)
	data := momo_common.ReplicationData{
		New:       newReplicationMode,
		TimeStamp: time.Now().UnixNano(),
	}

	if err := encoder.Encode(data); err != nil {
		log.Printf("Encode error: %v", err)
	}
}
