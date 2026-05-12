package local

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const apiKeyPrefix = "nx_live_"

// generateAPIKey produces a fresh API key plaintext, its stored prefix, and its
// SHA-256 hash. The plaintext is returned exactly once at creation; only the
// hash is persisted.
func generateAPIKey() (plaintext, prefix string, hash []byte, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return
	}
	tail := base64.RawURLEncoding.EncodeToString(b) // 32 chars
	plaintext = apiKeyPrefix + tail
	prefix = plaintext[:8] // "nx_live_"
	h := sha256.Sum256([]byte(plaintext))
	hash = h[:]
	return
}

func hashAPIKey(plaintext string) []byte {
	h := sha256.Sum256([]byte(plaintext))
	return h[:]
}
