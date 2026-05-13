package minio

import (
	"bytes"
	"context"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
)

// makeStore constructs a Store with a real local KeyVault but a nil MinIO
// client — the crypto tests don't touch the network. The encrypt/decrypt
// methods are package-private so this file lives in the same package.
func makeStore(t *testing.T) *Store {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	kv, err := keyvault.NewLocal(key)
	if err != nil {
		t.Fatalf("keyvault: %v", err)
	}
	return &Store{kv: kv}
}

func TestEnvelope_RoundTrip(t *testing.T) {
	s := makeStore(t)
	cases := [][]byte{
		[]byte(""),
		[]byte("hello"),
		bytes.Repeat([]byte{0xAB}, 4096),
	}
	for _, in := range cases {
		env, err := s.encrypt(context.Background(), in)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if len(env) < 8 {
			t.Fatalf("envelope too short")
		}
		if !bytes.Equal(env[:4], envelopeMagic) {
			t.Fatalf("envelope magic mismatch: %x", env[:4])
		}
		out, err := s.decrypt(context.Background(), env)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if !bytes.Equal(out, in) {
			t.Fatalf("round-trip mismatch: got %q want %q", out, in)
		}
	}
}

func TestEnvelope_TamperedCiphertextFails(t *testing.T) {
	s := makeStore(t)
	env, err := s.encrypt(context.Background(), []byte("hello"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Flip the last byte of the ciphertext.
	env[len(env)-1] ^= 0xFF
	if _, err := s.decrypt(context.Background(), env); err == nil {
		t.Fatalf("expected gcm.Open failure on tampered ciphertext")
	}
}

func TestEnvelope_BadMagic(t *testing.T) {
	s := makeStore(t)
	env, _ := s.encrypt(context.Background(), []byte("hello"))
	env[0] = 0xFF
	if _, err := s.decrypt(context.Background(), env); err == nil {
		t.Fatalf("expected bad-magic rejection")
	}
}

func TestEnvelope_TruncatedHeader(t *testing.T) {
	s := makeStore(t)
	if _, err := s.decrypt(context.Background(), []byte{0x01, 0x02}); err == nil {
		t.Fatalf("expected truncated-header rejection")
	}
}
