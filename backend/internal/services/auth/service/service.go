// Package service is the auth domain logic — HTTP-agnostic.
package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nexis/backend/internal/platform/authn"
	platerrors "nexis/backend/internal/platform/errors"
	"nexis/backend/internal/services/auth/adapter/clerk"
	"nexis/backend/internal/services/auth/model"
)

// Repository is the persistence interface — owned by the consumer (this package).
type Repository interface {
	UpsertUserFromClerk(ctx context.Context, clerkUserID, email, name, avatarURL string) (model.User, error)
	GetUserByClerkID(ctx context.Context, clerkUserID string) (model.User, error)
	SoftDeleteUserByClerkID(ctx context.Context, clerkUserID string) error

	UpsertOrgFromClerk(ctx context.Context, clerkOrgID, name, slug string) (model.Org, error)
	GetOrgByClerkID(ctx context.Context, clerkOrgID string) (model.Org, error)
	GetOrgByID(ctx context.Context, orgID string) (model.Org, error)

	UpsertMembership(ctx context.Context, orgID, userID string, role model.Role) error
	DeleteMembership(ctx context.Context, orgID, userID string) error
	MembershipsForUser(ctx context.Context, userID string) ([]model.OrgSummary, error)
	RoleOf(ctx context.Context, orgID, userID string) (model.Role, error)

	CreateAPIKey(ctx context.Context, k model.APIKey, hash string) error
	ListAPIKeys(ctx context.Context, orgID string) ([]model.APIKey, error)
	RevokeAPIKey(ctx context.Context, id, orgID string) error

	RecordWebhookEvent(ctx context.Context, eventID, eventType string, payload []byte) (bool, error)
}

type Service struct {
	repo                Repository
	clerkWebhookSecret  string
	apiKeyDefaultExpiry time.Duration
	environment         string
}

func New(repo Repository, opts Options) *Service {
	return &Service{
		repo:                repo,
		clerkWebhookSecret:  opts.ClerkWebhookSecret,
		apiKeyDefaultExpiry: time.Duration(opts.APIKeyDefaultExpiryDays) * 24 * time.Hour,
		environment:         opts.Environment,
	}
}

type Options struct {
	ClerkWebhookSecret      string
	APIKeyDefaultExpiryDays int
	Environment             string
}

// ProcessClerkWebhook validates signature, idempotency, then dispatches.
func (s *Service) ProcessClerkWebhook(ctx context.Context, headers map[string][]string, body []byte) error {
	if err := clerk.VerifyWebhook(s.clerkWebhookSecret, headers, body); err != nil {
		// Allow unsigned in dev for local testing only.
		if s.environment != "dev" {
			return platerrors.Wrap(platerrors.KindUnauthorized, "webhook signature invalid", err)
		}
	}
	var ev model.ClerkEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return platerrors.Wrap(platerrors.KindBadRequest, "invalid webhook payload", err)
	}
	if ev.ID == "" {
		// fall back to svix-id
		if h := headers["Svix-Id"]; len(h) > 0 {
			ev.ID = h[0]
		}
	}
	first, err := s.repo.RecordWebhookEvent(ctx, ev.ID, ev.Type, body)
	if err != nil {
		return platerrors.Wrap(platerrors.KindInternal, "record webhook", err)
	}
	if !first {
		return nil // duplicate, already processed
	}
	return s.dispatchClerkEvent(ctx, ev)
}

func (s *Service) dispatchClerkEvent(ctx context.Context, ev model.ClerkEvent) error {
	switch ev.Type {
	case "user.created", "user.updated":
		email := ""
		if len(ev.Data.EmailAddresses) > 0 {
			email = ev.Data.EmailAddresses[0].EmailAddress
		}
		name := strings.TrimSpace(ev.Data.FirstName + " " + ev.Data.LastName)
		_, err := s.repo.UpsertUserFromClerk(ctx, ev.Data.ID, email, name, ev.Data.ImageURL)
		return err

	case "user.deleted":
		return s.repo.SoftDeleteUserByClerkID(ctx, ev.Data.ID)

	case "organization.created", "organization.updated":
		_, err := s.repo.UpsertOrgFromClerk(ctx, ev.Data.ID, ev.Data.Name, ev.Data.Slug)
		return err

	case "organizationMembership.created", "organizationMembership.updated":
		if ev.Data.Organization == nil || ev.Data.PublicUserData == nil {
			return platerrors.New(platerrors.KindBadRequest, "membership event missing org/user")
		}
		org, err := s.repo.GetOrgByClerkID(ctx, ev.Data.Organization.ID)
		if err != nil {
			return err
		}
		user, err := s.repo.GetUserByClerkID(ctx, ev.Data.PublicUserData.UserID)
		if err != nil {
			return err
		}
		role := mapClerkRole(ev.Data.Role)
		return s.repo.UpsertMembership(ctx, org.ID, user.ID, role)

	case "organizationMembership.deleted":
		if ev.Data.Organization == nil || ev.Data.PublicUserData == nil {
			return platerrors.New(platerrors.KindBadRequest, "membership delete missing org/user")
		}
		org, err := s.repo.GetOrgByClerkID(ctx, ev.Data.Organization.ID)
		if err != nil {
			return err
		}
		user, err := s.repo.GetUserByClerkID(ctx, ev.Data.PublicUserData.UserID)
		if err != nil {
			return err
		}
		return s.repo.DeleteMembership(ctx, org.ID, user.ID)

	default:
		// silently accept unknown events; they're recorded in clerk_webhook_events for forensics
		return nil
	}
}

func mapClerkRole(clerkRole string) model.Role {
	switch clerkRole {
	case "org:admin", "admin":
		return model.RoleAdmin
	case "org:member":
		return model.RoleEngineer
	case "org:owner":
		return model.RoleOwner
	default:
		return model.RoleViewer
	}
}

// IssueAPIKey creates a new key, returns the plaintext (shown once to caller).
func (s *Service) IssueAPIKey(ctx context.Context, orgID, createdByUserID, name string, scopes []string, ttl time.Duration) (model.APIKey, string, error) {
	if name == "" {
		return model.APIKey{}, "", platerrors.New(platerrors.KindBadRequest, "name required")
	}
	if ttl == 0 {
		ttl = s.apiKeyDefaultExpiry
	}
	plain, prefix, err := generateAPIKey(s.environment)
	if err != nil {
		return model.APIKey{}, "", platerrors.Wrap(platerrors.KindInternal, "generate key", err)
	}
	hash, err := authn.HashAPIKey(plain)
	if err != nil {
		return model.APIKey{}, "", platerrors.Wrap(platerrors.KindInternal, "hash key", err)
	}
	k := model.APIKey{
		OrgID:           orgID,
		CreatedByUserID: createdByUserID,
		Name:            name,
		Prefix:          prefix,
		Scopes:          scopes,
		ExpiresAt:       time.Now().Add(ttl).UTC(),
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.repo.CreateAPIKey(ctx, k, hash); err != nil {
		return model.APIKey{}, "", platerrors.Wrap(platerrors.KindInternal, "persist key", err)
	}
	return k, plain, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, orgID string) ([]model.APIKey, error) {
	return s.repo.ListAPIKeys(ctx, orgID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, id, orgID string) error {
	return s.repo.RevokeAPIKey(ctx, id, orgID)
}

func (s *Service) Me(ctx context.Context, clerkUserID, orgID string) (model.Me, error) {
	user, err := s.repo.GetUserByClerkID(ctx, clerkUserID)
	if err != nil {
		return model.Me{}, err
	}
	memberships, err := s.repo.MembershipsForUser(ctx, user.ID)
	if err != nil {
		return model.Me{}, err
	}
	if orgID == "" {
		if len(memberships) > 0 {
			orgID = memberships[0].ID
		} else {
			return model.Me{User: user, Memberships: nil}, nil
		}
	}
	org, err := s.repo.GetOrgByID(ctx, orgID)
	if err != nil {
		return model.Me{}, err
	}
	role, err := s.repo.RoleOf(ctx, org.ID, user.ID)
	if err != nil {
		return model.Me{}, err
	}
	return model.Me{
		User:        user,
		Org:         org,
		Role:        role,
		Permissions: permissionsFor(role),
		Memberships: memberships,
	}, nil
}

func permissionsFor(role model.Role) []string {
	switch role {
	case model.RoleOwner, model.RoleAdmin:
		return []string{"*"}
	case model.RoleSRE:
		return []string{"incident:*", "policy:manage", "connector:manage", "agent:read", "audit:read"}
	case model.RoleEngineer:
		return []string{"incident:read", "incident:approve:own", "agent:read", "demo:run"}
	case model.RoleReviewer:
		return []string{"incident:read", "incident:approve:assigned", "agent:read", "audit:read"}
	case model.RoleViewer:
		return []string{"incident:read", "agent:read", "audit:read"}
	}
	return nil
}

func generateAPIKey(env string) (plain, prefix string, err error) {
	mode := "live"
	if env != "prod" {
		mode = "test"
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	body := base64.RawURLEncoding.EncodeToString(buf)
	plain = fmt.Sprintf("nxs_%s_%s", mode, body)
	if len(plain) >= 12 {
		prefix = plain[:12]
	} else {
		prefix = plain
	}
	return plain, prefix, nil
}
