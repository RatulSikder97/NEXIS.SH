// Package keyvault provides KeyVault implementations. The Phase 3 default is
// LocalKeyVault — AES-256-GCM with a process-local 32-byte master key sourced
// from the MASTER_KEY env (base64-decoded by cmd/server/main.go). The output
// is `nonce || gcm.Seal(plaintext)` so a single byte slice round-trips through
// Encrypt/Decrypt without callers needing to manage nonces.
package keyvault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

const keyLen = 32

// LocalKeyVault implements domain.KeyVault using AES-256-GCM. It is safe for
// concurrent use: the underlying cipher.AEAD returned by cipher.NewGCM is
// stateless and goroutine-safe.
type LocalKeyVault struct{ gcm cipher.AEAD }

// NewLocal constructs a LocalKeyVault from a 32-byte key. Anything shorter or
// longer is rejected — callers must base64-decode the env value before passing
// it in.
func NewLocal(key []byte) (*LocalKeyVault, error) {
	if len(key) != keyLen {
		return nil, errors.New("LocalKeyVault: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &LocalKeyVault{gcm: gcm}, nil
}

// Encrypt seals plaintext under a fresh random 12-byte nonce. The nonce is
// prepended to the ciphertext so Decrypt can recover it without out-of-band
// signalling.
func (k *LocalKeyVault) Encrypt(_ context.Context, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, k.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return k.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a ciphertext sealed by Encrypt. Any tampering (header or body)
// causes gcm.Open to return a non-nil error, which we propagate verbatim.
func (k *LocalKeyVault) Decrypt(_ context.Context, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < k.gcm.NonceSize() {
		return nil, errors.New("LocalKeyVault: ciphertext too short")
	}
	nonce, ct := ciphertext[:k.gcm.NonceSize()], ciphertext[k.gcm.NonceSize():]
	return k.gcm.Open(nil, nonce, ct, nil)
}
