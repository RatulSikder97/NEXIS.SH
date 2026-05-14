package secrets

// Coverage for the AWS Secrets Manager-backed store using a fake
// secretsManagerAPI. Exercises:
//
//   - Get(): SecretString, SecretBinary, and empty-payload branches.
//   - Put(): existing-secret update + ResourceNotFoundException create
//     fallback.
//   - fullName(): prefix join + trailing-slash trim.

import (
	"context"
	"errors"
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// fakeSMAPI is the in-memory secretsmanagerAPI stand-in. Tests configure
// per-call response shapes and inspect what was sent in.
type fakeSMAPI struct {
	getValue *secretsmanager.GetSecretValueOutput
	getErr   error
	putErr   error
	createOK bool
	createErr error

	gets    []string // SecretId received by GetSecretValue
	puts    []string // SecretId received by PutSecretValue
	creates []string // Name received by CreateSecret
}

func (f *fakeSMAPI) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	if in.SecretId != nil {
		f.gets = append(f.gets, *in.SecretId)
	}
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getValue, nil
}

func (f *fakeSMAPI) PutSecretValue(_ context.Context, in *secretsmanager.PutSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	if in.SecretId != nil {
		f.puts = append(f.puts, *in.SecretId)
	}
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &secretsmanager.PutSecretValueOutput{}, nil
}

func (f *fakeSMAPI) CreateSecret(_ context.Context, in *secretsmanager.CreateSecretInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
	if in.Name != nil {
		f.creates = append(f.creates, *in.Name)
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	if !f.createOK {
		return nil, errors.New("create disabled")
	}
	return &secretsmanager.CreateSecretOutput{}, nil
}

// TestSecretsManager_Get_StringPayload returns the SecretString bytes.
func TestSecretsManager_Get_StringPayload(t *testing.T) {
	val := "sk_value_abc"
	api := &fakeSMAPI{
		getValue: &secretsmanager.GetSecretValueOutput{SecretString: &val},
	}
	s := newSecretsManagerStoreWithAPI(api, "nexis-test/control-plane")
	out, err := s.Get(context.Background(), "stripe/api_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(out) != val {
		t.Fatalf("got %q want %q", out, val)
	}
	if len(api.gets) != 1 || api.gets[0] != "nexis-test/control-plane/stripe/api_key" {
		t.Fatalf("SecretId not prefixed correctly: %+v", api.gets)
	}
}

// TestSecretsManager_Get_BinaryPayload returns SecretBinary bytes.
func TestSecretsManager_Get_BinaryPayload(t *testing.T) {
	bin := []byte{0x01, 0x02, 0x03}
	api := &fakeSMAPI{
		getValue: &secretsmanager.GetSecretValueOutput{SecretBinary: bin},
	}
	s := newSecretsManagerStoreWithAPI(api, "")
	out, err := s.Get(context.Background(), "raw/blob")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytesEqualSM(out, bin) {
		t.Fatalf("got %x want %x", out, bin)
	}
	// Empty prefix → SecretId is the name verbatim (after leading-slash trim).
	if len(api.gets) != 1 || api.gets[0] != "raw/blob" {
		t.Fatalf("empty-prefix SecretId: %v", api.gets)
	}
}

// TestSecretsManager_Get_EmptyValueReturnsError — neither String nor Binary.
func TestSecretsManager_Get_EmptyValueReturnsError(t *testing.T) {
	api := &fakeSMAPI{
		getValue: &secretsmanager.GetSecretValueOutput{},
	}
	s := newSecretsManagerStoreWithAPI(api, "ns")
	if _, err := s.Get(context.Background(), "x"); err == nil {
		t.Fatalf("expected error for empty payload")
	}
}

// TestSecretsManager_Get_ErrorPropagates.
func TestSecretsManager_Get_ErrorPropagates(t *testing.T) {
	api := &fakeSMAPI{getErr: errors.New("network down")}
	s := newSecretsManagerStoreWithAPI(api, "ns")
	_, err := s.Get(context.Background(), "x")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "network down") {
		t.Fatalf("error did not wrap underlying: %v", err)
	}
}

// TestSecretsManager_Put_ExistingSucceeds — secret exists, Put returns nil.
func TestSecretsManager_Put_ExistingSucceeds(t *testing.T) {
	api := &fakeSMAPI{}
	s := newSecretsManagerStoreWithAPI(api, "ns")
	if err := s.Put(context.Background(), "x", []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(api.puts) != 1 {
		t.Fatalf("puts=%d want 1", len(api.puts))
	}
	if len(api.creates) != 0 {
		t.Fatalf("creates=%d want 0", len(api.creates))
	}
}

// TestSecretsManager_Put_NotFound_FallsThroughToCreate.
func TestSecretsManager_Put_NotFound_FallsThroughToCreate(t *testing.T) {
	api := &fakeSMAPI{
		putErr:   errors.New("ResourceNotFoundException: thing not found"),
		createOK: true,
	}
	s := newSecretsManagerStoreWithAPI(api, "ns")
	if err := s.Put(context.Background(), "y", []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(api.creates) != 1 {
		t.Fatalf("creates=%d want 1", len(api.creates))
	}
	if api.creates[0] != "ns/y" {
		t.Fatalf("create SecretId: %q", api.creates[0])
	}
}

// TestSecretsManager_Put_NotFoundCreateAlsoFails.
func TestSecretsManager_Put_NotFoundCreateAlsoFails(t *testing.T) {
	api := &fakeSMAPI{
		putErr:    errors.New("ResourceNotFoundException"),
		createErr: errors.New("AccessDenied"),
	}
	s := newSecretsManagerStoreWithAPI(api, "ns")
	if err := s.Put(context.Background(), "z", []byte("v")); err == nil {
		t.Fatalf("expected error chain")
	}
}

// TestSecretsManager_Put_OtherErrorPropagates — non-NotFound errors do not
// fall through to Create.
func TestSecretsManager_Put_OtherErrorPropagates(t *testing.T) {
	api := &fakeSMAPI{
		putErr: errors.New("ThrottlingException"),
	}
	s := newSecretsManagerStoreWithAPI(api, "ns")
	if err := s.Put(context.Background(), "x", []byte("v")); err == nil {
		t.Fatalf("expected propagation")
	}
	if len(api.creates) != 0 {
		t.Fatalf("create called %d times for throttling; want 0", len(api.creates))
	}
}

// TestSecretsManager_FullName_TrimsTrailingSlashAndLeading.
func TestSecretsManager_FullName_TrimsTrailingSlashAndLeading(t *testing.T) {
	cases := []struct {
		prefix, name, want string
	}{
		{"prefix/", "name", "prefix/name"},
		{"prefix///", "name", "prefix/name"},
		{"prefix", "/name", "prefix/name"},
		{"", "name", "name"},
		{"", "/name", "name"},
		{"prefix", "/a/b", "prefix/a/b"},
	}
	for _, tc := range cases {
		s := newSecretsManagerStoreWithAPI(&fakeSMAPI{}, tc.prefix)
		got := s.fullName(tc.name)
		if got != tc.want {
			t.Errorf("fullName(prefix=%q,name=%q)=%q want %q", tc.prefix, tc.name, got, tc.want)
		}
	}
}

// TestNewSecretsManager_ConstructsWithClient — exercises the production
// constructor with a real *secretsmanager.Client (no network call here).
func TestNewSecretsManager_ConstructsWithClient(t *testing.T) {
	client := secretsmanager.New(secretsmanager.Options{Region: "us-east-1"})
	s := NewSecretsManager(client, "ns/control-plane/")
	if s == nil {
		t.Fatalf("nil store")
	}
	if got := s.fullName("x"); got != "ns/control-plane/x" {
		t.Fatalf("fullName: %q", got)
	}
}

// _ retains awsv2 so the import stays even if we drop something later.
var _ = awsv2.String

// bytesEqualSM is a tiny util — we don't pull in bytes.Equal because the
// import overlap with other tests is fine but kept local for clarity.
func bytesEqualSM(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
