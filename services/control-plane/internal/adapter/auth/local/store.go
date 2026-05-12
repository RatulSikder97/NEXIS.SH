package local

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Store is the persistence boundary for the local auth adapter. The production
// implementation is PGStore (pgx-backed); tests use memStore for a deterministic,
// in-memory fake. All Store methods are expected to be safe for concurrent use.
type Store interface {
	// org + user + membership
	CreateOrganization(ctx context.Context, o *domain.Organization) error
	CreateUser(ctx context.Context, u *domain.User) error
	CreateMembership(ctx context.Context, orgID, userID string, role domain.Role) error
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUser(ctx context.Context, id string) (*domain.User, error)
	GetMembership(ctx context.Context, userID string) (orgID string, role domain.Role, err error)
	UpdateUserMFASecret(ctx context.Context, userID, secret string) error
	UpdateUserMFAEnabled(ctx context.Context, userID string, enabled bool) error
	ClearUserMFA(ctx context.Context, userID string) error

	// session
	CreateSession(ctx context.Context, s *domain.Session) error
	GetSession(ctx context.Context, id string) (*domain.Session, error)
	RevokeSession(ctx context.Context, id string) error

	// magic
	CreateMagicToken(ctx context.Context, hash []byte, userID, purpose string, expiresAt time.Time) error
	GetMagicToken(ctx context.Context, hash []byte) (userID, purpose string, expiresAt time.Time, usedAt *time.Time, err error)
	MarkMagicTokenUsed(ctx context.Context, hash []byte) error

	// api keys
	CreateAPIKey(ctx context.Context, k *domain.APIKey, hash []byte) error
	GetAPIKeyByHash(ctx context.Context, hash []byte) (*domain.APIKey, error)
	ListAPIKeysByOrg(ctx context.Context, orgID string) ([]domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, orgID, id string) error
}

// memStore is an in-memory Store fake used exclusively by unit tests. It
// returns deep copies for read methods so callers cannot mutate internal state.
type memStore struct {
	mu          sync.RWMutex
	orgs        map[string]domain.Organization
	users       map[string]domain.User
	usersByMail map[string]string // email → userID
	memberships map[string]membership
	sessions    map[string]domain.Session
	magic       map[string]magicEntry // hex(hash) → entry
	apiKeys     map[string]apiKeyEntry
	mailer      *testMailer
}

type membership struct {
	orgID string
	role  domain.Role
}

type magicEntry struct {
	hash      []byte
	userID    string
	purpose   string
	expiresAt time.Time
	usedAt    *time.Time
}

type apiKeyEntry struct {
	key  domain.APIKey
	hash []byte
}

func newMemStore() *memStore {
	return &memStore{
		orgs:        map[string]domain.Organization{},
		users:       map[string]domain.User{},
		usersByMail: map[string]string{},
		memberships: map[string]membership{},
		sessions:    map[string]domain.Session{},
		magic:       map[string]magicEntry{},
		apiKeys:     map[string]apiKeyEntry{},
	}
}

func (m *memStore) CreateOrganization(_ context.Context, o *domain.Organization) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.orgs[o.ID]; dup {
		return fmt.Errorf("memStore: org %q already exists: %w", o.ID, domain.ErrConflict)
	}
	cp := *o
	m.orgs[o.ID] = cp
	return nil
}

func (m *memStore) CreateUser(_ context.Context, u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.usersByMail[u.Email]; dup {
		return fmt.Errorf("memStore: user %q already exists: %w", u.Email, domain.ErrConflict)
	}
	cp := *u
	m.users[u.ID] = cp
	m.usersByMail[u.Email] = u.ID
	return nil
}

func (m *memStore) CreateMembership(_ context.Context, orgID, userID string, role domain.Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memberships[userID] = membership{orgID: orgID, role: role}
	return nil
}

func (m *memStore) GetUserByEmail(_ context.Context, email string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.usersByMail[email]
	if !ok {
		return nil, domain.ErrNotFound
	}
	u := m.users[id]
	return &u, nil
}

func (m *memStore) GetUser(_ context.Context, id string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &u, nil
}

func (m *memStore) GetMembership(_ context.Context, userID string) (string, domain.Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mb, ok := m.memberships[userID]
	if !ok {
		return "", "", domain.ErrNotFound
	}
	return mb.orgID, mb.role, nil
}

func (m *memStore) UpdateUserMFASecret(_ context.Context, userID, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return domain.ErrNotFound
	}
	u.MFASecret = secret
	u.MFAEnabled = false
	m.users[userID] = u
	return nil
}

func (m *memStore) UpdateUserMFAEnabled(_ context.Context, userID string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return domain.ErrNotFound
	}
	u.MFAEnabled = enabled
	m.users[userID] = u
	return nil
}

func (m *memStore) ClearUserMFA(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return domain.ErrNotFound
	}
	u.MFASecret = ""
	u.MFAEnabled = false
	m.users[userID] = u
	return nil
}

func (m *memStore) CreateSession(_ context.Context, s *domain.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.sessions[s.ID] = cp
	return nil
}

func (m *memStore) GetSession(_ context.Context, id string) (*domain.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &s, nil
}

func (m *memStore) RevokeSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	s.RevokedAt = &now
	m.sessions[id] = s
	return nil
}

func (m *memStore) CreateMagicToken(_ context.Context, hash []byte, userID, purpose string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hex.EncodeToString(hash)
	m.magic[k] = magicEntry{hash: append([]byte(nil), hash...), userID: userID, purpose: purpose, expiresAt: expiresAt}
	return nil
}

func (m *memStore) GetMagicToken(_ context.Context, hash []byte) (string, string, time.Time, *time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.magic[hex.EncodeToString(hash)]
	if !ok {
		return "", "", time.Time{}, nil, domain.ErrNotFound
	}
	var used *time.Time
	if e.usedAt != nil {
		t := *e.usedAt
		used = &t
	}
	return e.userID, e.purpose, e.expiresAt, used, nil
}

func (m *memStore) MarkMagicTokenUsed(_ context.Context, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hex.EncodeToString(hash)
	e, ok := m.magic[k]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	e.usedAt = &now
	m.magic[k] = e
	return nil
}

func (m *memStore) CreateAPIKey(_ context.Context, k *domain.APIKey, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *k
	if cp.Scopes != nil {
		cp.Scopes = append([]string(nil), k.Scopes...)
	}
	m.apiKeys[k.ID] = apiKeyEntry{key: cp, hash: append([]byte(nil), hash...)}
	return nil
}

func (m *memStore) GetAPIKeyByHash(_ context.Context, hash []byte) (*domain.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.apiKeys {
		if bytesEqual(e.hash, hash) {
			cp := e.key
			if e.key.Scopes != nil {
				cp.Scopes = append([]string(nil), e.key.Scopes...)
			}
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *memStore) ListAPIKeysByOrg(_ context.Context, orgID string) ([]domain.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.APIKey
	for _, e := range m.apiKeys {
		if e.key.OrgID != orgID {
			continue
		}
		cp := e.key
		if e.key.Scopes != nil {
			cp.Scopes = append([]string(nil), e.key.Scopes...)
		}
		out = append(out, cp)
	}
	return out, nil
}

func (m *memStore) RevokeAPIKey(_ context.Context, orgID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.apiKeys[id]
	if !ok || e.key.OrgID != orgID {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	e.key.RevokedAt = &now
	m.apiKeys[id] = e
	return nil
}

// getMembership is a test helper: returns the role of userID within orgID.
func (m *memStore) getMembership(orgID, userID string) (domain.Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mb, ok := m.memberships[userID]
	if !ok || mb.orgID != orgID {
		return "", domain.ErrNotFound
	}
	return mb.role, nil
}

// lastMagicToken returns the plaintext token captured by the test mailer in the
// most recent IssueMagicLink call. It extracts the token query parameter from
// the email link.
func (m *memStore) lastMagicToken() string {
	if m.mailer == nil {
		return ""
	}
	link := m.mailer.lastLink
	const marker = "token="
	idx := indexOf(link, marker)
	if idx < 0 {
		return ""
	}
	return link[idx+len(marker):]
}

func bytesEqual(a, b []byte) bool {
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

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
