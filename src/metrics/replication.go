// Package metrics provides the metrics collection and analysis functionality for the momo application.
package metrics

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// pushNewReplicationMode notifies all daemons in the cluster of a replication mode change.
// This ensures that in a Balanced Primary model, every node stays synchronized with the polymorphic state.
func pushNewReplicationMode(cfg common.Configuration, paddedAuthToken []byte, newReplicationMode int) {
	log.Printf("Notifying all daemons of new replication mode: %d", newReplicationMode)

	data := common.ReplicationData{
		New:       newReplicationMode,
		TimeStamp: time.Now().UnixNano(),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Encode error: %v", common.SanitizeLog(err.Error()))
		return
	}

	// ⚡ Bolt: Consolidate AuthToken and JSON payload.
	buf := make([]byte, 0, len(paddedAuthToken)+len(jsonData)+1)
	buf = append(buf, paddedAuthToken...)
	buf = append(buf, jsonData...)
	buf = append(buf, '\n')

	var wg sync.WaitGroup
	for i, d := range cfg.Daemons {
		wg.Add(1)
		go func(id int, addr string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("CRITICAL: Panic recovered in Metrics broadcast to node %d: %v", id, r)
				}
			}()

			conn, err := common.DialSocket(addr)
			if err != nil {
				log.Printf("Dial error for node %d (%s): %v", id, addr, common.SanitizeLog(err.Error()))
				return
			}
			defer conn.Close()

			if _, err := conn.Write(buf); err != nil {
				log.Printf("Failed to notify node %d: %v", id, common.SanitizeLog(err.Error()))
			}
		}(i, d.ChangeReplication)
	}
	wg.Wait()
}
