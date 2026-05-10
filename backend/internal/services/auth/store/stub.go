// Stub repo for local boot when DATABASE_URL is not set.
// All writes return errors; reads return empty.
package store

import (
	"context"
	"errors"

	"nexis/backend/internal/services/auth/model"
)

type Stub struct{}

var errNoDB = errors.New("auth: DATABASE_URL not configured")

func (Stub) UpsertUserFromClerk(context.Context, string, string, string, string) (model.User, error) {
	return model.User{}, errNoDB
}
func (Stub) GetUserByClerkID(context.Context, string) (model.User, error) { return model.User{}, errNoDB }
func (Stub) SoftDeleteUserByClerkID(context.Context, string) error        { return errNoDB }
func (Stub) UpsertOrgFromClerk(context.Context, string, string, string) (model.Org, error) {
	return model.Org{}, errNoDB
}
func (Stub) GetOrgByClerkID(context.Context, string) (model.Org, error) { return model.Org{}, errNoDB }
func (Stub) GetOrgByID(context.Context, string) (model.Org, error)      { return model.Org{}, errNoDB }
func (Stub) UpsertMembership(context.Context, string, string, model.Role) error { return errNoDB }
func (Stub) DeleteMembership(context.Context, string, string) error             { return errNoDB }
func (Stub) MembershipsForUser(context.Context, string) ([]model.OrgSummary, error) {
	return nil, nil
}
func (Stub) RoleOf(context.Context, string, string) (model.Role, error) { return "", errNoDB }
func (Stub) CreateAPIKey(context.Context, model.APIKey, string) error   { return errNoDB }
func (Stub) ListAPIKeys(context.Context, string) ([]model.APIKey, error) { return nil, nil }
func (Stub) RevokeAPIKey(context.Context, string, string) error         { return errNoDB }
func (Stub) RecordWebhookEvent(context.Context, string, string, []byte) (bool, error) {
	return false, errNoDB
}
