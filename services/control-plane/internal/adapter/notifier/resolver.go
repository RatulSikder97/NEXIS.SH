package notifier

import (
	"context"
)

// AdminEmailLister is the narrow port RepoResolver adapts onto the
// RecipientResolver interface. *repo.DigestRepo satisfies it via structural
// typing (AdminOwnerAdminEmails) — keeping the port here means the notifier
// package still takes no pgx dependency, exactly as the RecipientResolver
// doc comment in email.go prescribes.
type AdminEmailLister interface {
	AdminOwnerAdminEmails(ctx context.Context, orgID string) ([]string, error)
}

// RepoResolver is the production RecipientResolver: owner + admin emails for
// the org, resolved through the members repo.
type RepoResolver struct {
	Members AdminEmailLister
}

// NewRepoResolver wraps an AdminEmailLister as a RecipientResolver.
func NewRepoResolver(m AdminEmailLister) *RepoResolver {
	return &RepoResolver{Members: m}
}

// Resolve returns one Recipient per owner/admin email. A nil lister resolves
// to zero recipients so the notifier degrades to a no-op instead of erroring.
func (r *RepoResolver) Resolve(ctx context.Context, orgID string) ([]Recipient, error) {
	if r == nil || r.Members == nil {
		return nil, nil
	}
	emails, err := r.Members.AdminOwnerAdminEmails(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]Recipient, 0, len(emails))
	for _, e := range emails {
		if e == "" {
			continue
		}
		out = append(out, Recipient{Email: e})
	}
	return out, nil
}

// compile-time conformance check
var _ RecipientResolver = (*RepoResolver)(nil)
