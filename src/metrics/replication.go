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
// ⚡ Bolt: Accept configuration as an argument instead of calling GetConfigFromFile on every invocation to eliminate redundant file I/O and parsing overhead.
func pushNewReplicationMode(config momo_common.Configuration, newReplicationMode int) {
	log.Printf("Notifying primary daemon of new replication mode: %d", newReplicationMode)

	if len(config.Daemons) == 0 {
		log.Printf("No daemons configured")
		return
	}

	conn, err := momo_common.DialSocket(config.Daemons[0].ChangeReplication)
	if err != nil {
		log.Printf("Dial error: %v", err)
		return
	}
	defer conn.Close()

	// 🛡️ Sentinel: Send mandatory authentication token immediately upon connection
	if _, err := conn.Write([]byte(momo_common.PadString(config.Global.AuthToken, 64))); err != nil {
		log.Printf("Failed to send auth token: %v", err)
		return
	}

	encoder := json.NewEncoder(conn)
	data := momo_common.ReplicationData{
		New:       newReplicationMode,
		TimeStamp: time.Now().Unix(),
	}

	if err := encoder.Encode(data); err != nil {
		log.Printf("Encode error: %v", err)
	}
}
