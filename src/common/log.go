package common

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"
	"unsafe"

	"go.etcd.io/bbolt"
)

// LogStdOut configures the logging output for the application.
// If logApp is true, it sets the log flags to include timestamps, file names, and line numbers.
// If logApp is false, it discards all log output.
func LogStdOut(logApp bool) {
	if logApp {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile | log.LUTC)
	} else {
		log.SetOutput(io.Discard)
	}
}

// SanitizeLog strips control characters from a string to prevent log injection.
// All control bytes below 0x20 are replaced with '_' except tab (\x09) which is
// preserved. This neutralizes CRLF, null bytes, ANSI escape sequences (ESC \x1b),
// and other control characters that could manipulate terminals or hide log content.
func SanitizeLog(input string) string {
	if !hasControlByte(input) {
		return input
	}

	b := make([]byte, len(input))
	copy(b, input)
	for i := range b {
		if b[i] < 0x20 && b[i] != '\t' {
			b[i] = '_'
		}
	}
	// ⚡ Bolt: Eliminate string allocation overhead by using unsafe.String.
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func hasControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 && b != '\t' {
			return true
		}
	}
	return false
}

// AuditRotation logs a key rotation event to the audit log.
// This is called by the RotationManager on every rotation attempt.
// The entry is stored in the BoltDB audit_log bucket (R8, #936).
func AuditRotation(ctx context.Context, db *bbolt.DB, purpose, oldKeyID, newKeyID, operator, trigger string, success bool, errMsg string) error {
	if db == nil {
		return nil // audit DB not available, silently skip
	}
	entry := AuditRotationEntry{
		Timestamp:  time.Now().UTC(),
		Purpose:    purpose,
		OldKeyID:   oldKeyID,
		NewKeyID:   newKeyID,
		Operator:   operator,
		Trigger:    trigger, // "scheduled" or "manual"
		Success:    success,
		Error:      errMsg,
		RetryCount: 0,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal audit entry: %w", err)
	}
	return db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("audit_log"))
		if err != nil {
			return err
		}
		key := fmt.Sprintf("%d-%s", time.Now().UnixNano(), entry.NewKeyID)
		return b.Put([]byte(key), data)
	})
}

// AuditRotationEntry represents a key rotation audit log entry.
type AuditRotationEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Purpose    string    `json:"purpose"`    // "encryption", "auth", "e2ee", "oprf"
	OldKeyID   string    `json:"old_key_id"`
	NewKeyID   string    `json:"new_key_id"`
	Operator   string    `json:"operator"`   // "system" or user identifier
	Trigger    string    `json:"trigger"`    // "scheduled" or "manual"
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	RetryCount int       `json:"retry_count"`
}
