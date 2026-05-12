package domain

import "context"

// KeyVault is the port for symmetric envelope encryption of small secrets
// (webhook secrets, OAuth refresh tokens, third-party API tokens). The Phase 3
// implementation lives in internal/adapter/keyvault/local.go (AES-256-GCM with
// a process-local master key). Production deployments will swap this for a
// cloud KMS / HSM adapter without changing callers.
type KeyVault interface {
	Encrypt(ctx context.Context, plaintext []byte) (ciphertext []byte, err error)
	Decrypt(ctx context.Context, ciphertext []byte) (plaintext []byte, err error)
}
