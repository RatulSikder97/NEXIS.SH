package domain

import (
	"context"
	"time"
)

// PatchStore stores envelope-encrypted artifacts (patches, sandbox logs,
// reports) in object storage. Implementations in internal/adapter/patchstore/:
//   - minio (Phase 4, local dev)
//   - s3    (Phase 7, AWS)
//
// Both wrap the body in a versioned envelope sealed by a KeyVault data key.
type PatchStore interface {
	// EnsureBucket is idempotent; safe to call before every Put.
	EnsureBucket(ctx context.Context, name string) error

	// Put writes plaintext after envelope-encrypting it. Body is opaque bytes;
	// ContentType is recorded on the object metadata.
	Put(ctx context.Context, opts PutOptions) error

	// Get reverses Put — fetches the object, strips the envelope, returns plaintext.
	Get(ctx context.Context, bucket, key string) ([]byte, error)

	// SignURL issues a time-limited GET URL. The data is still encrypted at
	// rest; the URL grants ciphertext access only. Decryption happens
	// server-side via Get.
	SignURL(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

// PutOptions carries the arguments to PatchStore.Put. ContentType is recorded
// on the object metadata; the body itself is always encrypted before upload.
type PutOptions struct {
	Bucket      string
	Key         string
	Body        []byte
	ContentType string
}

// BucketForOrg returns the canonical bucket name "nexis-org-<orgID>".
// Stored separately so handlers / activities don't reimplement the convention.
func BucketForOrg(orgID string) string { return "nexis-org-" + orgID }
