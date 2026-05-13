package keyvault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// kmsWireVersion is the leading byte of the KMSVault envelope. Bumping it
// would let future readers reject mis-versioned blobs cleanly.
const kmsWireVersion byte = 0x01

// nonceSize is the AES-GCM nonce size in bytes (12). KMSVault hardcodes the
// expected nonce length so wire-parsing stays predictable.
const nonceSize = 12

// kmsAPI is the narrow surface KMSVault depends on. Decoupling from
// *kms.Client lets tests pass a fake (in-memory envelope) without spinning
// up localstack. The production constructor wraps the real client.
type kmsAPI interface {
	GenerateDataKey(ctx context.Context, in *kms.GenerateDataKeyInput, opts ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error)
	Decrypt(ctx context.Context, in *kms.DecryptInput, opts ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

// KMSVault implements domain.KeyVault via AWS KMS envelope encryption.
//
// Wire format: [1 ver][2 BE dek_len][dek][12 nonce][ciphertext].
//
// Every Encrypt call mints a fresh 256-bit data encryption key (DEK) via
// kms:GenerateDataKey, AES-GCM-seals the plaintext under that DEK, and stores
// the KMS-encrypted DEK alongside the ciphertext. Decrypt reverses the flow.
//
// EncryptionContext is bound to {org_id, secret_kind} drawn from the request
// context (domain.WithTenant + domain.WithSecretKind). KMS will REFUSE to
// decrypt under a different context — so a row sealed for `org=A,kind=webhook`
// is unrecoverable under `org=B`, even if an attacker exfiltrates the
// ciphertext.
type KMSVault struct {
	client kmsAPI
	keyARN string
}

// NewKMSVault constructs a KMSVault bound to keyARN. Both client and keyARN
// must be non-nil/empty; callers in main.go and the factory enforce this.
func NewKMSVault(client *kms.Client, keyARN string) *KMSVault {
	return &KMSVault{client: client, keyARN: keyARN}
}

// newKMSVaultWithAPI is the test-only constructor that injects a fake
// kmsAPI implementation. Production callers use NewKMSVault.
func newKMSVaultWithAPI(client kmsAPI, keyARN string) *KMSVault {
	return &KMSVault{client: client, keyARN: keyARN}
}

// compile-time conformance check
var _ domain.KeyVault = (*KMSVault)(nil)

// Encrypt envelope-encrypts plaintext. The DEK is zeroed after the seal call
// so it does not linger in memory once Encrypt returns.
//
// The returned ciphertext includes the KMS-encrypted DEK so Decrypt does not
// need any side-channel storage.
func (v *KMSVault) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	encCtx := v.encContext(ctx)

	out, err := v.client.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:             awsv2.String(v.keyARN),
		KeySpec:           kmstypes.DataKeySpecAes256,
		EncryptionContext: encCtx,
	})
	if err != nil {
		return nil, fmt.Errorf("kms generate-data-key: %w", err)
	}
	// zero the DEK once we're done with it
	defer func() {
		for i := range out.Plaintext {
			out.Plaintext[i] = 0
		}
	}()

	block, err := aes.NewCipher(out.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)

	dekLen := len(out.CiphertextBlob)
	if dekLen > 0xffff {
		return nil, fmt.Errorf("dek too large: %d bytes", dekLen)
	}

	buf := make([]byte, 0, 1+2+dekLen+len(nonce)+len(ct))
	buf = append(buf, kmsWireVersion)
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(dekLen))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, out.CiphertextBlob...)
	buf = append(buf, nonce...)
	buf = append(buf, ct...)
	return buf, nil
}

// Decrypt opens an envelope produced by Encrypt. EncryptionContext must
// match the context the row was sealed under; KMS enforces this server-side.
func (v *KMSVault) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 1+2+nonceSize {
		return nil, errors.New("kms vault: ciphertext too short")
	}
	if ciphertext[0] != kmsWireVersion {
		return nil, fmt.Errorf("kms vault: unknown wire version 0x%02x", ciphertext[0])
	}
	dekLen := int(binary.BigEndian.Uint16(ciphertext[1:3]))
	if len(ciphertext) < 3+dekLen+nonceSize {
		return nil, errors.New("kms vault: malformed wire (truncated)")
	}
	encDEK := ciphertext[3 : 3+dekLen]
	nonce := ciphertext[3+dekLen : 3+dekLen+nonceSize]
	ct := ciphertext[3+dekLen+nonceSize:]

	encCtx := v.encContext(ctx)
	out, err := v.client.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob:    encDEK,
		EncryptionContext: encCtx,
		KeyId:             awsv2.String(v.keyARN),
	})
	if err != nil {
		return nil, fmt.Errorf("kms decrypt: %w", err)
	}
	defer func() {
		for i := range out.Plaintext {
			out.Plaintext[i] = 0
		}
	}()

	block, err := aes.NewCipher(out.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aes gcm: %w", err)
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("aead open: %w", err)
	}
	return pt, nil
}

// encContext builds the KMS EncryptionContext map from values stitched into
// ctx via domain.WithTenant + domain.WithSecretKind. Empty values are
// omitted so existing callers (which don't yet thread tenant context) still
// work in dev — but production cuts MUST pin both, and the factory should
// gate that.
func (v *KMSVault) encContext(ctx context.Context) map[string]string {
	out := map[string]string{}
	if org := domain.TenantFromCtx(ctx); org != "" {
		out["org_id"] = org
	}
	if kind := domain.SecretKindFromCtx(ctx); kind != "" {
		out["secret_kind"] = kind
	}
	return out
}
