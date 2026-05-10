// Package model defines auth-service domain types.
package model

import "time"

type Role string

const (
	RoleOwner    Role = "owner"
	RoleAdmin    Role = "admin"
	RoleSRE      Role = "sre"
	RoleEngineer Role = "engineer"
	RoleReviewer Role = "reviewer"
	RoleViewer   Role = "viewer"
	RoleBot      Role = "bot"
)

type User struct {
	ID          string    `json:"id"`
	ClerkUserID string    `json:"clerk_user_id"`
	Email       string    `json:"email"`
	Name        string    `json:"name,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Org struct {
	ID          string    `json:"id"`
	ClerkOrgID  string    `json:"clerk_org_id,omitempty"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Plan        string    `json:"plan"`
	CreatedAt   time.Time `json:"created_at"`
}

type Membership struct {
	OrgID  string `json:"org_id"`
	UserID string `json:"user_id"`
	Role   Role   `json:"role"`
}

type APIKey struct {
	ID              string    `json:"id"`
	OrgID           string    `json:"org_id"`
	CreatedByUserID string    `json:"created_by_user_id"`
	Name            string    `json:"name"`
	Prefix          string    `json:"prefix"`
	Scopes          []string  `json:"scopes"`
	ExpiresAt       time.Time `json:"expires_at"`
	CreatedAt       time.Time `json:"created_at"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

type Me struct {
	User        User     `json:"user"`
	Org         Org      `json:"org"`
	Role        Role     `json:"role"`
	Permissions []string `json:"permissions"`
	Memberships []OrgSummary `json:"memberships"`
}

type OrgSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role Role   `json:"role"`
}

// Clerk webhook payload subset we care about.
type ClerkEvent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Data      ClerkEventData  `json:"data"`
}

type ClerkEventData struct {
	ID            string                  `json:"id"`
	EmailAddresses []ClerkEmail            `json:"email_addresses,omitempty"`
	FirstName     string                  `json:"first_name,omitempty"`
	LastName      string                  `json:"last_name,omitempty"`
	ImageURL      string                  `json:"image_url,omitempty"`
	Slug          string                  `json:"slug,omitempty"`
	Name          string                  `json:"name,omitempty"`
	Organization  *ClerkOrganization      `json:"organization,omitempty"`
	PublicUserData *ClerkUserData         `json:"public_user_data,omitempty"`
	Role          string                  `json:"role,omitempty"`
}

type ClerkEmail struct {
	EmailAddress string `json:"email_address"`
	ID           string `json:"id"`
}

type ClerkOrganization struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type ClerkUserData struct {
	UserID string `json:"user_id"`
}
