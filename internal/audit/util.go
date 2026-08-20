package audit

import (
	"crypto/rand"
	"encoding/hex"
)

// randHex returns n random hex bytes (2n hex chars).
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
