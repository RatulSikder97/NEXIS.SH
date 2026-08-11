package keyvault

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeKMS is an in-memory stand-in for *kms.Client used by the KMSVault
// tests. It enforces the same EncryptionContext-matching contract that real
// KMS enforces: Decrypt rejects a CiphertextBlob whose stored context does
// not match the one passed by the caller.
//
// Wire format of the fake CiphertextBlob: [32 raw DEK][JSON encContext].
// The real KMS blob is opaque; the test only depends on round-trip + ctx
// rejection.
type fakeKMS struct {
	masterKey []byte // 32 bytes; used to AES-GCM-wrap the DEK so we don't return plaintext as the blob
}

func newFakeKMS() *fakeKMS {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		panic(err)
	}
	return &fakeKMS{masterKey: key}
}

// blobEnvelope wraps a generated DEK and its bound EncryptionContext in a
// JSON payload, then AES-GCM-seals it with the fake's master key. This way
// the CiphertextBlob returned by GenerateDataKey can't be forged without the
// fake's master key, mirroring real KMS semantics.
type blobEnvelope struct {
	DEK    []byte            `json:"dek"`
	EncCtx map[string]string `json:"enc_ctx"`
}

func (f *fakeKMS) wrap(env blobEnvelope) ([]byte, error) {
	raw, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(f.masterKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, raw, nil)...), nil
}

func (f *fakeKMS) unwrap(blob []byte) (blobEnvelope, error) {
	if len(blob) < 12 {
		return blobEnvelope{}, errors.New("fakeKMS: blob too short")
	}
	block, err := aes.NewCipher(f.masterKey)
	if err != nil {
		return blobEnvelope{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return blobEnvelope{}, err
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	raw, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return blobEnvelope{}, err
	}
	var env blobEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return blobEnvelope{}, err
	}
	return env, nil
}

func (f *fakeKMS) GenerateDataKey(_ context.Context, in *kms.GenerateDataKeyInput, _ ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, err
	}
	blob, err := f.wrap(blobEnvelope{DEK: bytes.Clone(dek), EncCtx: in.EncryptionContext})
	if err != nil {
		return nil, err
	}
	return &kms.GenerateDataKeyOutput{
		Plaintext:      dek,
		CiphertextBlob: blob,
	}, nil
}

func (f *fakeKMS) Decrypt(_ context.Context, in *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	env, err := f.unwrap(in.CiphertextBlob)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(env.EncCtx, in.EncryptionContext) {
		return nil, errors.New("fakeKMS: InvalidCiphertextException — EncryptionContext mismatch")
	}
	return &kms.DecryptOutput{Plaintext: env.DEK}, nil
}

func TestKMSVault_RoundTrip(t *testing.T) {
	t.Parallel()
	fake := newFakeKMS()
	v := newKMSVaultWithAPI(fake, "arn:aws:kms:us-east-1:000:key/test")

	ctx := domain.WithSecretKind(domain.WithTenant(context.Background(), "org-A"), "webhook")
	pt := bytes.Repeat([]byte("hello-nexis-"), 100)

	ct, err := v.Encrypt(ctx, pt)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := v.Decrypt(ctx, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(pt, got) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestKMSVault_OneMegRoundTrip(t *testing.T) {
	t.Parallel()
	fake := newFakeKMS()
	v := newKMSVaultWithAPI(fake, "arn:aws:kms:us-east-1:000:key/test")

	ctx := domain.WithSecretKind(domain.WithTenant(context.Background(), "org-1mb"), "patch")
	pt := make([]byte, 1024*1024)
	if _, err := io.ReadFull(rand.Reader, pt); err != nil {
		t.Fatalf("rand: %v", err)
	}

	ct, err := v.Encrypt(ctx, pt)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := v.Decrypt(ctx, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(pt, got) {
		t.Fatalf("1MB round-trip mismatch")
	}
}

func TestKMSVault_DifferentSecretKindSucceedsIndependently(t *testing.T) {
	t.Parallel()
	fake := newFakeKMS()
	v := newKMSVaultWithAPI(fake, "arn:aws:kms:us-east-1:000:key/test")

	ctxA := domain.WithSecretKind(domain.WithTenant(context.Background(), "org-X"), "webhook")
	ctxB := domain.WithSecretKind(domain.WithTenant(context.Background(), "org-X"), "oauth_refresh")

	pa, _ := v.Encrypt(ctxA, []byte("alpha"))
	pb, _ := v.Encrypt(ctxB, []byte("bravo"))

	// Each opens under its own ctx.
	if got, err := v.Decrypt(ctxA, pa); err != nil || string(got) != "alpha" {
		t.Fatalf("ctxA decrypt: got=%q err=%v", got, err)
	}
	if got, err := v.Decrypt(ctxB, pb); err != nil || string(got) != "bravo" {
		t.Fatalf("ctxB decrypt: got=%q err=%v", got, err)
	}

	// Cross-decrypt must fail.
	if _, err := v.Decrypt(ctxA, pb); err == nil {
		t.Fatalf("expected mismatch error decrypting webhook-ctxt under oauth_refresh-kind")
	}
}

func TestKMSVault_CrossTenantRefused(t *testing.T) {
	t.Parallel()
	fake := newFakeKMS()
	v := newKMSVaultWithAPI(fake, "arn:aws:kms:us-east-1:000:key/test")

	ctxA := domain.WithSecretKind(domain.WithTenant(context.Background(), "org-A"), "webhook")
	ctxB := domain.WithSecretKind(domain.WithTenant(context.Background(), "org-B"), "webhook")

	ct, err := v.Encrypt(ctxA, []byte("secret-for-A"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := v.Decrypt(ctxB, ct); err == nil {
		t.Fatalf("cross-tenant decrypt should have been refused")
	}
}

func TestKMSVault_TruncatedCiphertext(t *testing.T) {
	t.Parallel()
	fake := newFakeKMS()
	v := newKMSVaultWithAPI(fake, "arn:aws:kms:us-east-1:000:key/test")

	if _, err := v.Decrypt(context.Background(), []byte{}); err == nil {
		t.Fatalf("expected error on empty input")
	}
	if _, err := v.Decrypt(context.Background(), []byte{0x99, 0, 0}); err == nil {
		t.Fatalf("expected error on wrong wire version")
	}
}
