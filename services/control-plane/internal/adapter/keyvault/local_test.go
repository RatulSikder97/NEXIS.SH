package keyvault

import (
	"bytes"
	"context"
	"testing"
)

func TestLocalKeyVault_RoundTrip(t *testing.T) {
	kv, err := NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("hello secret world")
	ct, err := kv.Encrypt(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct, msg) {
		t.Fatal("ciphertext == plaintext")
	}
	pt, err := kv.Decrypt(context.Background(), ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, msg) {
		t.Errorf("roundtrip mismatch: got %q", pt)
	}
}

func TestLocalKeyVault_TamperedCiphertextFails(t *testing.T) {
	kv, _ := NewLocal(make([]byte, 32))
	ct, _ := kv.Encrypt(context.Background(), []byte("x"))
	ct[len(ct)-1] ^= 0xff
	if _, err := kv.Decrypt(context.Background(), ct); err == nil {
		t.Fatal("want error on tampered ciphertext")
	}
}

func TestLocalKeyVault_RequiresKey32(t *testing.T) {
	if _, err := NewLocal(make([]byte, 16)); err == nil {
		t.Fatal("want error on short key")
	}
}
