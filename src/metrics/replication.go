package metrics

import (
	"encoding/json"
	"log"
	"net"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

// pushNewReplicationMode connects to the primary server and sends a notification to change the replication mode.
// ⚡ Bolt: Pass the pre-padded AuthToken to avoid redundant padding/allocation in each call.
func pushNewReplicationMode(cfg momo_common.Configuration, paddedAuthToken []byte, newMode int) {
	daemons := cfg.Daemons
	log.Printf("Notifying primary daemon of new replication mode: %d", newMode)

	conn, err := net.Dial("tcp", daemons[0].ChangeReplication)
	if err != nil {
		log.Printf("Dial error: %v", err)
		return
	}
	defer conn.Close()

	data := momo_common.ReplicationData{
		Old:       -1, // Not used by the primary server to update its own state
		New:       newMode,
		TimeStamp: time.Now().UnixNano(),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Marshal error: %v", err)
		return
	}

	// ⚡ Bolt: Consolidate AuthToken and JSON payload into a single optimally-sized buffer
	// to avoid multiple `conn.Write` calls and `json.NewEncoder` allocation overhead.
	buf := make([]byte, 0, len(paddedAuthToken)+len(jsonData)+1)
	buf = append(buf, paddedAuthToken...)
	buf = append(buf, jsonData...)
	buf = append(buf, '\n') // Add trailing newline for `json.Decoder` compatibility on the server

	if _, err := conn.Write(buf); err != nil {
		log.Printf("Failed to send AuthToken and ReplicationData: %v", err)
		return
	}
}
