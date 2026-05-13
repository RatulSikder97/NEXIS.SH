package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// secretsManagerAPI is the narrow surface SecretsManagerStore depends on.
// Decoupling from *secretsmanager.Client lets tests inject a fake.
type secretsManagerAPI interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	PutSecretValue(ctx context.Context, in *secretsmanager.PutSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
	CreateSecret(ctx context.Context, in *secretsmanager.CreateSecretInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error)
}

// SecretsManagerStore implements domain.SecretsStore against AWS Secrets
// Manager. Secret names are joined to a prefix (typically
// "nexis-<env>/control-plane/") so the IAM policy can scope reads to the
// service's namespace.
type SecretsManagerStore struct {
	client secretsManagerAPI
	prefix string
}

// NewSecretsManager constructs a Store with the given prefix. prefix is
// trimmed of trailing slashes; Get + Put join with "/".
func NewSecretsManager(client *secretsmanager.Client, prefix string) *SecretsManagerStore {
	return &SecretsManagerStore{
		client: client,
		prefix: strings.TrimRight(prefix, "/"),
	}
}

// newSecretsManagerStoreWithAPI is the test-only constructor accepting a
// faked secretsManagerAPI.
func newSecretsManagerStoreWithAPI(client secretsManagerAPI, prefix string) *SecretsManagerStore {
	return &SecretsManagerStore{
		client: client,
		prefix: strings.TrimRight(prefix, "/"),
	}
}

// compile-time conformance check
var _ domain.SecretsStore = (*SecretsManagerStore)(nil)

// Get returns the SecretString bytes (or SecretBinary if the secret was
// stored as binary) for prefix/name. Errors propagate verbatim — callers
// should treat any error as "secret not available" without distinguishing
// 404s from transient.
func (s *SecretsManagerStore) Get(ctx context.Context, name string) ([]byte, error) {
	full := s.fullName(name)
	out, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: awsv2.String(full),
	})
	if err != nil {
		return nil, fmt.Errorf("secrets-manager get %q: %w", full, err)
	}
	if out.SecretString != nil {
		return []byte(*out.SecretString), nil
	}
	if out.SecretBinary != nil {
		return out.SecretBinary, nil
	}
	return nil, errors.New("secrets-manager: empty SecretValue")
}

// Put writes value as the latest version of prefix/name. If the secret
// doesn't exist yet, the SDK's PutSecretValue surfaces a
// ResourceNotFoundException; we then fall back to CreateSecret. Existing
// versions are not deleted (Secrets Manager keeps the version history
// indefinitely until rotation policy prunes them).
func (s *SecretsManagerStore) Put(ctx context.Context, name string, value []byte) error {
	full := s.fullName(name)
	_, err := s.client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     awsv2.String(full),
		SecretString: awsv2.String(string(value)),
	})
	if err == nil {
		return nil
	}
	// Best-effort: treat a "ResourceNotFoundException" string in the error
	// as the trigger for CreateSecret. The smithy-go API error surface is
	// SDK-stable; we match by substring because the typed error is in an
	// internal package.
	if strings.Contains(err.Error(), "ResourceNotFoundException") ||
		strings.Contains(err.Error(), "Secrets Manager can't find") {
		_, cerr := s.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
			Name:         awsv2.String(full),
			SecretString: awsv2.String(string(value)),
		})
		if cerr != nil {
			return fmt.Errorf("secrets-manager create %q: %w", full, cerr)
		}
		return nil
	}
	return fmt.Errorf("secrets-manager put %q: %w", full, err)
}

// fullName joins prefix and name. prefix is allowed to be empty (the legacy
// dev-style "no prefix" case), in which case name passes through unchanged.
func (s *SecretsManagerStore) fullName(name string) string {
	name = strings.TrimLeft(name, "/")
	if s.prefix == "" {
		return name
	}
	return s.prefix + "/" + name
}
