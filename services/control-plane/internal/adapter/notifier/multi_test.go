package notifier_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/notifier"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type fakeNotifier struct {
	mu      sync.Mutex
	name    string
	failErr error
	calls   []domain.Notification
}

func (f *fakeNotifier) Channel() string { return f.name }
func (f *fakeNotifier) Send(_ context.Context, n domain.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, n)
	return f.failErr
}

func TestMulti_FansOutToEveryChild(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, b, c := &fakeNotifier{name: "slack"}, &fakeNotifier{name: "email"}, &fakeNotifier{name: "console"}
	m := notifier.NewMulti(logger, a, b, c)

	err := m.Send(context.Background(), domain.Notification{OrgID: "org-1", Kind: domain.NotifApprovalRequested})
	require.NoError(t, err)
	require.Len(t, a.calls, 1)
	require.Len(t, b.calls, 1)
	require.Len(t, c.calls, 1)
}

func TestMulti_AbsorbsPerChildErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bad := &fakeNotifier{name: "slack", failErr: errors.New("503")}
	good := &fakeNotifier{name: "email"}
	m := notifier.NewMulti(logger, bad, good)

	require.NoError(t, m.Send(context.Background(), domain.Notification{OrgID: "org-1"}))
	require.Len(t, bad.calls, 1, "bad notifier still received the call")
	require.Len(t, good.calls, 1, "good notifier still runs even when bad failed")
}

func TestMulti_ReturnsNilWhenNoChildren(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := notifier.NewMulti(logger)
	require.NoError(t, m.Send(context.Background(), domain.Notification{}))
	require.Equal(t, "multi", m.Channel())
}
