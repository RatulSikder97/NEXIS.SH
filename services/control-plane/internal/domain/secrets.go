package domain

import "context"

// SecretsStore is the Phase 7 port for fetching service-wide secrets (Stripe
// key, WorkOS API key, GitHub App private key, Resend/SES API key). Two
// implementations live in internal/adapter/secrets/*:
//
//   - env     — reads os.Getenv (dev / compose default).
//   - awssm   — reads AWS Secrets Manager (staging/prod).
//
// The interface is intentionally tiny — Phase 7 does not need write paths
// during normal operation; rotation tooling will call Put via a separate
// admin CLI.
type SecretsStore interface {
	// Get returns the raw bytes of the secret named name. The implementation
	// MAY scope name with a configured prefix (e.g.
	// "nexis-staging/control-plane/stripe.api_key").
	Get(ctx context.Context, name string) ([]byte, error)

	// Put writes value as the latest version of the secret named name.
	Put(ctx context.Context, name string, value []byte) error
}

// secretsCtxKey is the context key used to thread the AWS KMS
// EncryptionContext through KeyVault.Encrypt/Decrypt calls without changing
// the KeyVault port signature. KMSVault reads OrgID + SecretKind from ctx via
// the helpers below.
type secretsCtxKey int

const (
	ctxKeyOrgID secretsCtxKey = iota
	ctxKeySecretKind
)

// WithTenant returns a ctx with the org id pinned. KMSVault uses it as the
// `org_id` key in EncryptionContext so a ciphertext sealed under one tenant
// cannot be decrypted under another.
func WithTenant(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, ctxKeyOrgID, orgID)
}

// WithSecretKind returns a ctx with the secret kind pinned (e.g. "webhook",
// "oauth_refresh"). KMSVault uses it as `secret_kind` in EncryptionContext.
func WithSecretKind(ctx context.Context, kind string) context.Context {
	return context.WithValue(ctx, ctxKeySecretKind, kind)
}

// TenantFromCtx returns the org id pinned by WithTenant, or "" when unset.
func TenantFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyOrgID).(string); ok {
		return v
	}
	return ""
}

// SecretKindFromCtx returns the secret kind pinned by WithSecretKind, or ""
// when unset.
func SecretKindFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeySecretKind).(string); ok {
		return v
	}
	return ""
}
