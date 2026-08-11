package local

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Store is the persistence boundary for the local auth adapter. The production
// implementation is PGStore (pgx-backed); tests use MemStore for a deterministic,
// in-memory fake. All Store methods are expected to be safe for concurrent use.
type Store interface {
	// org + user + membership
	CreateOrganization(ctx context.Context, o *domain.Organization) error
	CreateUser(ctx context.Context, u *domain.User) error
	CreateMembership(ctx context.Context, orgID, userID string, role domain.Role) error
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUser(ctx context.Context, id string) (*domain.User, error)
	GetOrganization(ctx context.Context, id string) (*domain.Organization, error)
	GetMembership(ctx context.Context, userID string) (orgID string, role domain.Role, err error)
	UpdateUserMFASecret(ctx context.Context, userID, secret string) error
	UpdateUserMFAEnabled(ctx context.Context, userID string, enabled bool) error
	ClearUserMFA(ctx context.Context, userID string) error

	// session
	CreateSession(ctx context.Context, s *domain.Session) error
	GetSession(ctx context.Context, id string) (*domain.Session, error)
	RevokeSession(ctx context.Context, id string) error
	// Phase 9 — session management. ListSessionsByUser returns only live
	// rows (unrevoked, unexpired at `now`), newest first. TouchSession
	// stamps last_seen_at; RevokeSessionsForUser bulk-revokes every live
	// session (used after a password reset) and is a no-op when none exist.
	ListSessionsByUser(ctx context.Context, userID string, now time.Time) ([]domain.Session, error)
	TouchSession(ctx context.Context, id string, at time.Time) error
	RevokeSessionsForUser(ctx context.Context, userID string) error

	// magic
	CreateMagicToken(ctx context.Context, hash []byte, userID, purpose string, expiresAt time.Time) error
	GetMagicToken(ctx context.Context, hash []byte) (userID, purpose string, expiresAt time.Time, usedAt *time.Time, err error)
	MarkMagicTokenUsed(ctx context.Context, hash []byte) error

	// Phase 9 — password reset tokens. Same hash-at-rest contract as magic
	// tokens: callers pass the SHA-256 of the plaintext, never the plaintext.
	CreatePasswordResetToken(ctx context.Context, hash []byte, userID string, expiresAt time.Time) error
	GetPasswordResetToken(ctx context.Context, hash []byte) (userID string, expiresAt time.Time, consumedAt *time.Time, err error)
	MarkPasswordResetConsumed(ctx context.Context, hash []byte) error
	UpdateUserPassword(ctx context.Context, userID, passwordHash string) error

	// api keys
	CreateAPIKey(ctx context.Context, k *domain.APIKey, hash []byte) error
	GetAPIKeyByHash(ctx context.Context, hash []byte) (*domain.APIKey, error)
	ListAPIKeysByOrg(ctx context.Context, orgID string) ([]domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, orgID, id string) error

	// Phase 3 — invites
	CreateInvite(ctx context.Context, i *domain.Invite) error
	GetInvite(ctx context.Context, tokenHash []byte) (*domain.Invite, error)
	ListInvites(ctx context.Context, orgID string) ([]domain.Invite, error)
	MarkInviteClaimed(ctx context.Context, tokenHash []byte) error
	DeleteInvite(ctx context.Context, tokenHash []byte) error

	// Phase 3 Stage 8 — user preferences (theme, etc.). Stored in the
	// users.preferences jsonb column. GetUserPreferences returns an empty
	// map when none have been persisted yet; UpdateUserPreferences performs
	// a last-write-wins overwrite of the column.
	GetUserPreferences(ctx context.Context, userID string) (map[string]any, error)
	UpdateUserPreferences(ctx context.Context, userID string, prefs map[string]any) error
}

// MemStore is an in-memory Store fake used exclusively by unit tests. It
// returns deep copies for read methods so callers cannot mutate internal state.
type MemStore struct {
	mu          sync.RWMutex
	orgs        map[string]domain.Organization
	users       map[string]domain.User
	usersByMail map[string]string // email → userID
	memberships map[string]membership
	sessions    map[string]domain.Session
	magic       map[string]magicEntry // hex(hash) → entry
	apiKeys     map[string]apiKeyEntry
	invites     map[string]domain.Invite  // hex(tokenHash) → invite
	prefs       map[string]map[string]any // userID → preferences
	resets      map[string]resetEntry     // hex(hash) → entry
	mailer      *TestMailer
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

type resetEntry struct {
	userID     string
	expiresAt  time.Time
	consumedAt *time.Time
}

func NewMemStore() *MemStore {
	return &MemStore{
		orgs:        map[string]domain.Organization{},
		users:       map[string]domain.User{},
		usersByMail: map[string]string{},
		memberships: map[string]membership{},
		sessions:    map[string]domain.Session{},
		magic:       map[string]magicEntry{},
		apiKeys:     map[string]apiKeyEntry{},
		invites:     map[string]domain.Invite{},
		prefs:       map[string]map[string]any{},
		resets:      map[string]resetEntry{},
	}
}

func (m *MemStore) CreateOrganization(_ context.Context, o *domain.Organization) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.orgs[o.ID]; dup {
		return fmt.Errorf("MemStore: org %q already exists: %w", o.ID, domain.ErrConflict)
	}
	cp := *o
	m.orgs[o.ID] = cp
	return nil
}

func (m *MemStore) CreateUser(_ context.Context, u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.usersByMail[u.Email]; dup {
		return fmt.Errorf("MemStore: user %q already exists: %w", u.Email, domain.ErrConflict)
	}
	cp := *u
	m.users[u.ID] = cp
	m.usersByMail[u.Email] = u.ID
	return nil
}

func (m *MemStore) CreateMembership(_ context.Context, orgID, userID string, role domain.Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memberships[userID] = membership{orgID: orgID, role: role}
	return nil
}

func (m *MemStore) GetUserByEmail(_ context.Context, email string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.usersByMail[email]
	if !ok {
		return nil, domain.ErrNotFound
	}
	u := m.users[id]
	return &u, nil
}

func (m *MemStore) GetUser(_ context.Context, id string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &u, nil
}

func (m *MemStore) GetOrganization(_ context.Context, id string) (*domain.Organization, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.orgs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &o, nil
}

func (m *MemStore) GetMembership(_ context.Context, userID string) (string, domain.Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mb, ok := m.memberships[userID]
	if !ok {
		return "", "", domain.ErrNotFound
	}
	return mb.orgID, mb.role, nil
}

func (m *MemStore) UpdateUserMFASecret(_ context.Context, userID, secret string) error {
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

func (m *MemStore) UpdateUserMFAEnabled(_ context.Context, userID string, enabled bool) error {
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

func (m *MemStore) ClearUserMFA(_ context.Context, userID string) error {
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

func (m *MemStore) CreateSession(_ context.Context, s *domain.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.sessions[s.ID] = cp
	return nil
}

func (m *MemStore) GetSession(_ context.Context, id string) (*domain.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &s, nil
}

func (m *MemStore) RevokeSession(_ context.Context, id string) error {
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

// ListSessionsByUser returns live sessions (unrevoked, unexpired at now) for
// userID, newest first. Copies are returned so callers can't mutate state.
func (m *MemStore) ListSessionsByUser(_ context.Context, userID string, now time.Time) ([]domain.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.Session{}
	for _, s := range m.sessions {
		if s.UserID != userID || s.RevokedAt != nil || now.After(s.ExpiresAt) {
			continue
		}
		cp := s
		if s.LastSeenAt != nil {
			t := *s.LastSeenAt
			cp.LastSeenAt = &t
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// TouchSession stamps last_seen_at on the row. Missing rows are a no-op —
// the touch is best-effort telemetry, not a correctness path.
func (m *MemStore) TouchSession(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return domain.ErrNotFound
	}
	t := at
	s.LastSeenAt = &t
	m.sessions[id] = s
	return nil
}

// RevokeSessionsForUser marks every live session for userID revoked. No error
// when the user has none — the reset flow calls this unconditionally.
func (m *MemStore) RevokeSessionsForUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for id, s := range m.sessions {
		if s.UserID != userID || s.RevokedAt != nil {
			continue
		}
		t := now
		s.RevokedAt = &t
		m.sessions[id] = s
	}
	return nil
}

func (m *MemStore) CreateMagicToken(_ context.Context, hash []byte, userID, purpose string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hex.EncodeToString(hash)
	m.magic[k] = magicEntry{hash: append([]byte(nil), hash...), userID: userID, purpose: purpose, expiresAt: expiresAt}
	return nil
}

func (m *MemStore) GetMagicToken(_ context.Context, hash []byte) (string, string, time.Time, *time.Time, error) {
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

func (m *MemStore) MarkMagicTokenUsed(_ context.Context, hash []byte) error {
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

// --- password reset tokens (Phase 9) ---------------------------------------

// CreatePasswordResetToken persists a reset entry keyed by hex(hash),
// mirroring PGStore's bytea-PK table.
func (m *MemStore) CreatePasswordResetToken(_ context.Context, hash []byte, userID string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resets[hex.EncodeToString(hash)] = resetEntry{userID: userID, expiresAt: expiresAt}
	return nil
}

// GetPasswordResetToken returns the entry for the given hash, or ErrNotFound.
func (m *MemStore) GetPasswordResetToken(_ context.Context, hash []byte) (string, time.Time, *time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.resets[hex.EncodeToString(hash)]
	if !ok {
		return "", time.Time{}, nil, domain.ErrNotFound
	}
	var consumed *time.Time
	if e.consumedAt != nil {
		t := *e.consumedAt
		consumed = &t
	}
	return e.userID, e.expiresAt, consumed, nil
}

// MarkPasswordResetConsumed stamps consumed_at exactly once; a second call
// returns ErrNotFound, matching PGStore's `AND consumed_at IS NULL` guard.
func (m *MemStore) MarkPasswordResetConsumed(_ context.Context, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hex.EncodeToString(hash)
	e, ok := m.resets[k]
	if !ok || e.consumedAt != nil {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	e.consumedAt = &now
	m.resets[k] = e
	return nil
}

// UpdateUserPassword overwrites the stored bcrypt hash for userID.
func (m *MemStore) UpdateUserPassword(_ context.Context, userID, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return domain.ErrNotFound
	}
	u.PasswordHash = passwordHash
	m.users[userID] = u
	return nil
}

func (m *MemStore) CreateAPIKey(_ context.Context, k *domain.APIKey, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *k
	if cp.Scopes != nil {
		cp.Scopes = append([]string(nil), k.Scopes...)
	}
	m.apiKeys[k.ID] = apiKeyEntry{key: cp, hash: append([]byte(nil), hash...)}
	return nil
}

func (m *MemStore) GetAPIKeyByHash(_ context.Context, hash []byte) (*domain.APIKey, error) {
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

func (m *MemStore) ListAPIKeysByOrg(_ context.Context, orgID string) ([]domain.APIKey, error) {
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

func (m *MemStore) RevokeAPIKey(_ context.Context, orgID, id string) error {
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

// --- invites (Phase 3) -----------------------------------------------------

// CreateInvite persists a pending invite. Keyed by hex(tokenHash) so the
// MemStore lookup mirrors PGStore's bytea-keyed table.
func (m *MemStore) CreateInvite(_ context.Context, i *domain.Invite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hex.EncodeToString(i.TokenHash)
	cp := *i
	cp.TokenHash = append([]byte(nil), i.TokenHash...)
	m.invites[k] = cp
	return nil
}

// GetInvite returns the invite row for the given token hash, or ErrNotFound.
func (m *MemStore) GetInvite(_ context.Context, tokenHash []byte) (*domain.Invite, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k := hex.EncodeToString(tokenHash)
	v, ok := m.invites[k]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := v
	cp.TokenHash = append([]byte(nil), v.TokenHash...)
	if v.ClaimedAt != nil {
		t := *v.ClaimedAt
		cp.ClaimedAt = &t
	}
	return &cp, nil
}

// ListInvites returns all invites for the org, newest expiry first. Includes
// claimed + expired rows — the UI surfaces them with a status badge.
func (m *MemStore) ListInvites(_ context.Context, orgID string) ([]domain.Invite, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.Invite{}
	for _, v := range m.invites {
		if v.OrgID != orgID {
			continue
		}
		cp := v
		cp.TokenHash = append([]byte(nil), v.TokenHash...)
		if v.ClaimedAt != nil {
			t := *v.ClaimedAt
			cp.ClaimedAt = &t
		}
		out = append(out, cp)
	}
	return out, nil
}

// MarkInviteClaimed stamps claimed_at on the row. Idempotent — claiming an
// already-claimed row is a noop here; the higher layer (ClaimInvite on
// Provider) is responsible for the precondition check.
func (m *MemStore) MarkInviteClaimed(_ context.Context, tokenHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hex.EncodeToString(tokenHash)
	v, ok := m.invites[k]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	v.ClaimedAt = &now
	m.invites[k] = v
	return nil
}

// DeleteInvite removes the row. Used by RevokeInvite.
func (m *MemStore) DeleteInvite(_ context.Context, tokenHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hex.EncodeToString(tokenHash)
	if _, ok := m.invites[k]; !ok {
		return domain.ErrNotFound
	}
	delete(m.invites, k)
	return nil
}

// GetUserPreferences returns a shallow copy of the user's stored preferences
// map. Returns an empty (non-nil) map when none have been set.
func (m *MemStore) GetUserPreferences(_ context.Context, userID string) (map[string]any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.prefs[userID]
	if !ok {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out, nil
}

// UpdateUserPreferences performs a last-write-wins overwrite of the user's
// preferences. Stores a defensive copy so subsequent caller mutations don't
// leak back into the in-memory store.
func (m *MemStore) UpdateUserPreferences(_ context.Context, userID string, prefs map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]any, len(prefs))
	for k, v := range prefs {
		cp[k] = v
	}
	m.prefs[userID] = cp
	return nil
}

// getMembership is a test helper: returns the role of userID within orgID.
func (m *MemStore) getMembership(orgID, userID string) (domain.Role, error) {
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
func (m *MemStore) lastMagicToken() string {
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
