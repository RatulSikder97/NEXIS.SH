// Package minio implements domain.PatchStore on top of MinIO. Every Put
// envelope-encrypts the body before upload; Get reverses the process. The
// wire format is versioned:
//
//	[4 bytes magic "NX\x01\x00"][4 bytes BE wrapped_key_len][wrapped_key][12 bytes nonce][gcm ciphertext+tag]
//
// Phase 7's S3 adapter re-uses the same format byte-for-byte; only the
// transport changes. The data key is generated fresh per Put and wrapped via
// domain.KeyVault (LocalKeyVault in Phase 4 → KMS in Phase 7).
package minio

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// envelopeMagic is the version marker at byte 0..3 of every stored object.
// `NX\x01\x00` — Phase 7's S3 adapter rejects unknown magics so old patches
// remain readable after the migration.
var envelopeMagic = []byte{'N', 'X', 0x01, 0x00}

// nonceSize is the GCM standard 12-byte nonce length.
const nonceSize = 12

// Store is the MinIO-backed PatchStore. Zero value is unusable; construct via
// New.
type Store struct {
	client *miniogo.Client
	kv     domain.KeyVault
	region string
}

// Config bundles connection params + the KeyVault used to wrap data keys.
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Region    string // e.g. "us-east-1"; MinIO accepts an empty region
	KeyVault  domain.KeyVault
}

// New constructs a Store. The MinIO client itself defers network IO until the
// first call, so this can be used in main even when the broker is starting up.
func New(cfg Config) (*Store, error) {
	if cfg.KeyVault == nil {
		return nil, errors.New("patchstore/minio: KeyVault required")
	}
	c, err := miniogo.New(cfg.Endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("patchstore/minio: %w", err)
	}
	return &Store{client: c, kv: cfg.KeyVault, region: cfg.Region}, nil
}

// compile-time conformance check
var _ domain.PatchStore = (*Store)(nil)

// EnsureBucket is idempotent — multiple concurrent callers race safely.
func (s *Store) EnsureBucket(ctx context.Context, name string) error {
	exists, err := s.client.BucketExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, name, miniogo.MakeBucketOptions{Region: s.region}); err != nil {
		// Race: another goroutine created it just now.
		if exists2, _ := s.client.BucketExists(ctx, name); exists2 {
			return nil
		}
		return err
	}
	return nil
}

// Put envelope-encrypts opts.Body and uploads to MinIO.
func (s *Store) Put(ctx context.Context, opts domain.PutOptions) error {
	if err := s.EnsureBucket(ctx, opts.Bucket); err != nil {
		return err
	}
	envelope, err := s.encrypt(ctx, opts.Body)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, opts.Bucket, opts.Key,
		bytes.NewReader(envelope), int64(len(envelope)),
		miniogo.PutObjectOptions{ContentType: opts.ContentType})
	return err
}

// Get downloads the envelope, validates the magic, unwraps the data key, and
// decrypts the ciphertext.
func (s *Store) Get(ctx context.Context, bucket, key string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, bucket, key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	envelope, err := io.ReadAll(obj)
	if err != nil {
		return nil, err
	}
	return s.decrypt(ctx, envelope)
}

// SignURL grants ciphertext-only access for ttl. The caller still has to
// call Get to decrypt — the URL is useful for one-shot ciphertext downloads
// (audit / forensic dumps), not for clients that need plaintext.
func (s *Store) SignURL(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, bucket, key, ttl, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// encrypt builds the envelope:
//
//	[magic(4)][wrapped_key_len BE uint32(4)][wrapped_key][nonce(12)][ciphertext+tag]
func (s *Store) encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return nil, err
	}
	wrapped, err := s.kv.Encrypt(ctx, dataKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	var hdr [8]byte
	copy(hdr[0:4], envelopeMagic)
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(wrapped)))

	out := make([]byte, 0, len(hdr)+len(wrapped)+len(nonce)+len(ciphertext))
	out = append(out, hdr[:]...)
	out = append(out, wrapped...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// decrypt reverses encrypt. Bad magic / truncated envelopes / GCM tag
// failures all bubble up verbatim — callers (PatchStore.Get) propagate.
func (s *Store) decrypt(ctx context.Context, env []byte) ([]byte, error) {
	if len(env) < 8 {
		return nil, errors.New("envelope: truncated header")
	}
	if !bytes.Equal(env[:4], envelopeMagic) {
		return nil, fmt.Errorf("envelope: bad magic %x", env[:4])
	}
	wrappedLen := int(binary.BigEndian.Uint32(env[4:8]))
	if 8+wrappedLen+nonceSize > len(env) {
		return nil, errors.New("envelope: truncated body")
	}
	wrapped := env[8 : 8+wrappedLen]
	nonce := env[8+wrappedLen : 8+wrappedLen+nonceSize]
	ct := env[8+wrappedLen+nonceSize:]

	dataKey, err := s.kv.Decrypt(ctx, wrapped)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ct, nil)
}
