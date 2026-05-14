package keyvault

// Coverage for NewFromConfig. The "local" branch is exercised through a
// 32-byte master key; the "kms" branch requires the AWS SDK, so we only
// verify the config-validation error paths there.

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

// TestNewFromConfig_LocalHappyPath — a 32-byte master key produces a
// working LocalKeyVault.
func TestNewFromConfig_LocalHappyPath(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	cfg := config.Config{
		KeyVault:  "local",
		MasterKey: base64.StdEncoding.EncodeToString(key),
	}
	kv, err := NewFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if kv == nil {
		t.Fatalf("kv must be non-nil")
	}
}

// TestNewFromConfig_LocalEmptyBackendDefaults — empty backend string
// defaults to "local".
func TestNewFromConfig_LocalEmptyBackendDefaults(t *testing.T) {
	key := make([]byte, 32)
	cfg := config.Config{
		KeyVault:  "", // default to local
		MasterKey: base64.StdEncoding.EncodeToString(key),
	}
	kv, err := NewFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if kv == nil {
		t.Fatalf("kv must be non-nil")
	}
}

// TestNewFromConfig_LocalMissingKey — local requires MASTER_KEY.
func TestNewFromConfig_LocalMissingKey(t *testing.T) {
	cfg := config.Config{KeyVault: "local", MasterKey: ""}
	_, err := NewFromConfig(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error for missing master key")
	}
}

// TestNewFromConfig_LocalBadBase64 — malformed base64 must error.
func TestNewFromConfig_LocalBadBase64(t *testing.T) {
	cfg := config.Config{KeyVault: "local", MasterKey: "not-real-base64!"}
	_, err := NewFromConfig(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error for bad base64")
	}
}

// TestNewFromConfig_KMSMissingARN — kms backend requires KMS_KEY_ARN.
func TestNewFromConfig_KMSMissingARN(t *testing.T) {
	cfg := config.Config{KeyVault: "kms", KMSKeyARN: ""}
	_, err := NewFromConfig(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error for missing kms key arn")
	}
}

// TestNewFromConfig_UnknownBackend — anything else errors.
func TestNewFromConfig_UnknownBackend(t *testing.T) {
	cfg := config.Config{KeyVault: "azure"}
	_, err := NewFromConfig(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected error for unknown backend")
	}
}
