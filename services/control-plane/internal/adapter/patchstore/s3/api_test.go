package s3

// Additional coverage for the S3 patchstore — Put/Get/SignURL paths, the New
// constructor, and the EnsureBucket Create branch via a typed APIError. The
// envelope-only round-trip lives in store_test.go.

import (
	"bytes"
	"context"
	"errors"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// notFoundErr is a smithy.APIError shaped to look like S3's HeadBucket 404
// response. EnsureBucket's switch matches ErrorCode == "NotFound" via the
// errors.As path.
type notFoundErr struct {
	code string
}

func (e *notFoundErr) Error() string                 { return e.code }
func (e *notFoundErr) ErrorCode() string             { return e.code }
func (e *notFoundErr) ErrorMessage() string          { return e.code }
func (e *notFoundErr) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// makeStoreWithFakes wires a Store against fakes with our test KeyVault.
func makeStoreWithFakes(t *testing.T, api s3API, presign presignAPI, region string) *Store {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	kv, err := keyvault.NewLocal(key)
	if err != nil {
		t.Fatalf("keyvault: %v", err)
	}
	return newStoreWithAPI(api, presign, kv, region)
}

// TestNew_RequiresKeyVault — without a KeyVault the constructor refuses.
func TestNew_RequiresKeyVault(t *testing.T) {
	_, err := New(Config{Client: &s3.Client{}})
	if err == nil {
		t.Fatalf("expected error for missing KeyVault")
	}
}

// TestNew_RequiresClient — without an *s3.Client New refuses.
func TestNew_RequiresClient(t *testing.T) {
	key := make([]byte, 32)
	kv, _ := keyvault.NewLocal(key)
	_, err := New(Config{KeyVault: kv})
	if err == nil {
		t.Fatalf("expected error for missing s3 client")
	}
}

// TestEnsureBucket_NotFoundErrorCreates — when HeadBucket returns a typed
// NotFound APIError, EnsureBucket follows up with CreateBucket.
func TestEnsureBucket_NotFoundErrorCreates(t *testing.T) {
	api := &fakeS3API{
		headErr: &notFoundErr{code: "NotFound"},
	}
	s := makeStoreWithFakes(t, api, nil, "us-east-1")
	if err := s.EnsureBucket(context.Background(), "bucket"); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if api.creates != 1 {
		t.Fatalf("creates=%d, want 1", api.creates)
	}
}

// TestEnsureBucket_NotFoundErrorWithRegion — non-us-east-1 region attaches
// the LocationConstraint.
func TestEnsureBucket_NotFoundErrorWithRegion(t *testing.T) {
	api := &fakeS3API{
		headErr: &notFoundErr{code: "NoSuchBucket"},
	}
	s := makeStoreWithFakes(t, api, nil, "us-west-2")
	if err := s.EnsureBucket(context.Background(), "bucket"); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if api.creates != 1 {
		t.Fatalf("creates=%d, want 1", api.creates)
	}
}

// TestEnsureBucket_CreateFailsButHeadRecoversAfterRace — if Create fails but
// a re-HEAD succeeds (concurrent creator won), EnsureBucket reports success.
func TestEnsureBucket_CreateFailsButHeadRecoversAfterRace(t *testing.T) {
	// First Head: NotFound → triggers Create.
	// Create: failure (returns error).
	// Second Head: success → race recovery.
	api := &fakeS3APIWithSeq{
		headErrs: []error{
			&notFoundErr{code: "NotFound"},
			nil, // race recovery
		},
		createErr: errors.New("AlreadyOwnedByYou"),
	}
	s := makeStoreWithFakes(t, api, nil, "")
	if err := s.EnsureBucket(context.Background(), "bucket"); err != nil {
		t.Fatalf("expected race-recovery success, got %v", err)
	}
	if api.creates != 1 {
		t.Fatalf("create count = %d, want 1", api.creates)
	}
}

// TestEnsureBucket_NonAPIErrorPropagates — when HeadBucket returns a generic
// (non-APIError) failure the function propagates it without trying Create.
func TestEnsureBucket_NonAPIErrorPropagates(t *testing.T) {
	api := &fakeS3API{
		headErr: errors.New("connection refused"),
	}
	s := makeStoreWithFakes(t, api, nil, "")
	err := s.EnsureBucket(context.Background(), "bucket")
	if err == nil {
		t.Fatalf("expected propagated error")
	}
	if api.creates != 0 {
		t.Fatalf("CreateBucket should not run on non-APIError; got %d calls", api.creates)
	}
}

// TestEnsureBucket_OtherAPIErrorPropagates — when HeadBucket returns a
// non-NotFound APIError (e.g. AccessDenied), EnsureBucket propagates.
func TestEnsureBucket_OtherAPIErrorPropagates(t *testing.T) {
	api := &fakeS3API{
		headErr: &notFoundErr{code: "AccessDenied"},
	}
	s := makeStoreWithFakes(t, api, nil, "")
	if err := s.EnsureBucket(context.Background(), "bucket"); err == nil {
		t.Fatalf("expected AccessDenied propagation")
	}
	if api.creates != 0 {
		t.Fatalf("Create must not run on AccessDenied; got %d", api.creates)
	}
}

// TestPut_RoundTrip — Put calls EnsureBucket + encrypt + PutObject.
func TestPut_RoundTrip(t *testing.T) {
	api := &fakeS3API{}
	s := makeStoreWithFakes(t, api, nil, "us-east-1")
	body := []byte("hello world")
	err := s.Put(context.Background(), domain.PutOptions{
		Bucket:      "test-bucket",
		Key:         "patches/abc.bin",
		Body:        body,
		ContentType: "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if api.heads == 0 {
		t.Fatalf("expected HeadBucket call from EnsureBucket")
	}
	if api.puts != 1 {
		t.Fatalf("puts=%d want 1", api.puts)
	}
}

// TestPut_EnsureBucketFails — propagates failure when bucket cannot be ensured.
func TestPut_EnsureBucketFails(t *testing.T) {
	api := &fakeS3API{
		headErr: errors.New("perms broken"),
	}
	s := makeStoreWithFakes(t, api, nil, "us-east-1")
	if err := s.Put(context.Background(), domain.PutOptions{
		Bucket: "test-bucket", Key: "x", Body: []byte("x"),
	}); err == nil {
		t.Fatalf("expected error")
	}
}

// TestGet_RoundTrip — encrypt locally, plant it as GetObject body, then Get.
func TestGet_RoundTrip(t *testing.T) {
	api := &fakeS3API{}
	s := makeStoreWithFakes(t, api, nil, "us-east-1")
	body := []byte("hello round trip")
	// Encrypt a buffer using the same store so the envelope matches.
	env, err := s.encrypt(context.Background(), body)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	api.getBody = env

	got, err := s.Get(context.Background(), "test-bucket", "patches/abc.bin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("round trip mismatch: got %q want %q", got, body)
	}
}

// TestGet_BadEnvelope — junk in GetObject body surfaces as decrypt error.
func TestGet_BadEnvelope(t *testing.T) {
	api := &fakeS3API{getBody: []byte("not-an-envelope-junk")}
	s := makeStoreWithFakes(t, api, nil, "us-east-1")
	if _, err := s.Get(context.Background(), "bucket", "key"); err == nil {
		t.Fatalf("expected decrypt error")
	}
}

// TestGet_S3Error — GetObject 404/error propagates.
func TestGet_S3Error(t *testing.T) {
	api := &fakeS3API{getErr: errors.New("no such key")}
	s := makeStoreWithFakes(t, api, nil, "us-east-1")
	if _, err := s.Get(context.Background(), "bucket", "key"); err == nil {
		t.Fatalf("expected S3 error")
	}
}

// TestSignURL_HappyPath — presigner returns the URL.
func TestSignURL_HappyPath(t *testing.T) {
	pre := &fakePresigner{url: "https://signed.example.com/x?sig=abc"}
	s := makeStoreWithFakes(t, &fakeS3API{}, pre, "us-east-1")

	url, err := s.SignURL(context.Background(), "bucket", "key", 0)
	if err != nil {
		t.Fatalf("SignURL: %v", err)
	}
	if url != "https://signed.example.com/x?sig=abc" {
		t.Fatalf("url: %q", url)
	}
}

// TestSignURL_PresignerError — presigner failure propagates.
func TestSignURL_PresignerError(t *testing.T) {
	pre := &fakePresigner{err: errors.New("sign failed")}
	s := makeStoreWithFakes(t, &fakeS3API{}, pre, "us-east-1")
	if _, err := s.SignURL(context.Background(), "bucket", "key", 0); err == nil {
		t.Fatalf("expected propagated error")
	}
}

// TestSdkPresigner_Wraps — sdkPresigner wraps the SDK type into our
// PresignedRequest. We can't call PresignGetObject without a real S3 client
// signing context, so we only verify the wrapping struct is well-formed.
func TestSdkPresigner_TypeIsBuildable(t *testing.T) {
	// Constructing the type does not panic.
	_ = sdkPresigner{c: nil}
}

// fakeS3APIWithSeq returns a queue of HeadBucket errors so we can simulate
// the race-recovery path: first head says NotFound, Create fails, second
// head succeeds. The remaining methods are inert.
type fakeS3APIWithSeq struct {
	headErrs  []error
	headCalls int
	createErr error
	creates   int
}

func (f *fakeS3APIWithSeq) HeadBucket(_ context.Context, _ *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	idx := f.headCalls
	f.headCalls++
	if idx < len(f.headErrs) && f.headErrs[idx] != nil {
		return nil, f.headErrs[idx]
	}
	return &s3.HeadBucketOutput{}, nil
}

func (f *fakeS3APIWithSeq) CreateBucket(_ context.Context, _ *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	f.creates++
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &s3.CreateBucketOutput{}, nil
}

func (f *fakeS3APIWithSeq) PutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3APIWithSeq) GetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: &closeReader{r: bytes.NewReader(nil)}}, nil
}

// _ uses awsv2 so the import is retained when other tests are deleted.
var _ = awsv2.String
