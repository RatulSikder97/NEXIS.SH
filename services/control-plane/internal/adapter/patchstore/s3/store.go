// Package s3 implements domain.PatchStore on top of AWS S3 (SDK v2). The
// envelope wire format is identical to the MinIO peer — see
// internal/adapter/patchstore/minio/store.go — so a patch sealed in the
// compose path can be read back from S3 verbatim during the cutover and
// vice versa.
//
// Bucket pattern: nexis-org-<orgID>. The factory ensures the bucket exists
// via EnsureBucket before the first Put.
package s3

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

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// envelopeMagic must match the MinIO peer's marker byte-for-byte.
var envelopeMagic = []byte{'N', 'X', 0x01, 0x00}

const nonceSize = 12

// s3API is the narrow surface Store depends on. Decoupling from *s3.Client
// lets tests inject a fake without launching localstack.
type s3API interface {
	HeadBucket(ctx context.Context, in *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, in *s3.CreateBucketInput, opts ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// presigner is the narrow surface SignURL depends on. The SDK splits
// presign into a separate client; we accept it via a function field so the
// factory can wire whatever signing client the deployment uses.
type presignAPI interface {
	PresignGetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*PresignedRequest, error)
}

// PresignedRequest is the trimmed shape we need from the SDK's presign
// helper. The SDK returns a *v4.PresignedHTTPRequest; we shrink to URL +
// Method + SignedHeader so tests don't need to depend on the v4 type.
type PresignedRequest struct {
	URL    string
	Method string
}

// Store implements domain.PatchStore on top of AWS S3.
type Store struct {
	client    s3API
	presigner presignAPI
	region    string
	kv        domain.KeyVault
}

// Config bundles the deps needed to construct a Store.
type Config struct {
	Client    *s3.Client
	Presigner *s3.PresignClient
	Region    string
	KeyVault  domain.KeyVault
}

// New constructs a Store. KeyVault is required; pre-built S3 + Presign
// clients are expected from the cmd/server wiring path.
func New(cfg Config) (*Store, error) {
	if cfg.KeyVault == nil {
		return nil, errors.New("patchstore/s3: KeyVault required")
	}
	if cfg.Client == nil {
		return nil, errors.New("patchstore/s3: s3 client required")
	}
	presign := cfg.Presigner
	if presign == nil {
		presign = s3.NewPresignClient(cfg.Client)
	}
	return &Store{
		client:    cfg.Client,
		presigner: sdkPresigner{presign},
		region:    cfg.Region,
		kv:        cfg.KeyVault,
	}, nil
}

// newStoreWithAPI is the test-only constructor that injects fakes for
// both the s3API and presignAPI surfaces.
func newStoreWithAPI(api s3API, p presignAPI, kv domain.KeyVault, region string) *Store {
	return &Store{client: api, presigner: p, region: region, kv: kv}
}

// sdkPresigner wraps the real SDK *s3.PresignClient to fit presignAPI.
type sdkPresigner struct{ c *s3.PresignClient }

func (s sdkPresigner) PresignGetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*PresignedRequest, error) {
	r, err := s.c.PresignGetObject(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return &PresignedRequest{URL: r.URL, Method: r.Method}, nil
}

// compile-time conformance check
var _ domain.PatchStore = (*Store)(nil)

// EnsureBucket creates the bucket idempotently. HEAD first; on 404 (or
// NoSuchBucket / NotFound) we issue CreateBucket. Race-safe: a concurrent
// creator's success short-circuits us on retry.
func (s *Store) EnsureBucket(ctx context.Context, name string) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: awsv2.String(name)})
	if err == nil {
		return nil
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.ErrorCode() {
	case "NotFound", "NoSuchBucket":
		input := &s3.CreateBucketInput{Bucket: awsv2.String(name)}
		// us-east-1 must NOT carry a CreateBucketConfiguration block.
		if s.region != "" && s.region != "us-east-1" {
			input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
				LocationConstraint: s3types.BucketLocationConstraint(s.region),
			}
		}
		if _, cerr := s.client.CreateBucket(ctx, input); cerr != nil {
			// re-check — another goroutine may have raced us.
			if _, herr := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: awsv2.String(name)}); herr == nil {
				return nil
			}
			return cerr
		}
		return nil
	default:
		return err
	}
}

// Put envelope-encrypts opts.Body and uploads to S3 with SSE-S3 (AES256)
// turned on at rest. The bucket-level KMS policy supersedes this when the
// bucket itself is configured with aws:kms — S3 picks the stricter rule.
func (s *Store) Put(ctx context.Context, opts domain.PutOptions) error {
	if err := s.EnsureBucket(ctx, opts.Bucket); err != nil {
		return err
	}
	envelope, err := s.encrypt(ctx, opts.Body)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:               awsv2.String(opts.Bucket),
		Key:                  awsv2.String(opts.Key),
		Body:                 bytes.NewReader(envelope),
		ContentType:          awsv2.String(opts.ContentType),
		ServerSideEncryption: s3types.ServerSideEncryptionAes256,
	})
	return err
}

// Get downloads the envelope, validates the magic, unwraps the data key,
// and decrypts the ciphertext.
func (s *Store) Get(ctx context.Context, bucket, key string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: awsv2.String(bucket),
		Key:    awsv2.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	envelope, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}
	return s.decrypt(ctx, envelope)
}

// SignURL grants ciphertext-only access for ttl via a presigned GET URL.
// Decryption still happens server-side via Get.
func (s *Store) SignURL(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	r, err := s.presigner.PresignGetObject(ctx,
		&s3.GetObjectInput{Bucket: awsv2.String(bucket), Key: awsv2.String(key)},
		func(o *s3.PresignOptions) { o.Expires = ttl },
	)
	if err != nil {
		return "", err
	}
	return r.URL, nil
}

// encrypt is identical to the MinIO peer's encrypt — the envelope format
// must be byte-compatible.
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
