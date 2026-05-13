package handler_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
)

// fakeApprovals records every Decide invocation and lets tests inject a
// failure on demand. Implements handler.SlackApprovalsService.
type fakeApprovals struct {
	mu      sync.Mutex
	calls   []decideCall
	failErr error
}

type decideCall struct {
	RunID    string
	Decision domain.ApprovalDecisionState
	Email    string
}

func (f *fakeApprovals) Decide(_ context.Context, runID string, d domain.ApprovalDecisionState, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return f.failErr
	}
	f.calls = append(f.calls, decideCall{RunID: runID, Decision: d, Email: email})
	return nil
}

func (f *fakeApprovals) snapshot() []decideCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]decideCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// signRequest builds the X-Slack-Signature value for the supplied body.
func signRequest(t *testing.T, secret, ts, body string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("v0:" + ts + ":" + body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

// buildPayload assembles the form-encoded body Slack sends. The "payload"
// field's value is a JSON string. We URL-encode the outer body — same as
// Slack does on the wire — so the signature check sees the raw bytes
// uniformly.
func buildPayload(t *testing.T, actionID, value, userEmail string) string {
	t.Helper()
	inner, err := json.Marshal(map[string]any{
		"type": "block_actions",
		"user": map[string]any{
			"id":       "U001",
			"username": "alice",
			"email":    userEmail,
		},
		"team": map[string]any{"id": "T001"},
		"actions": []map[string]any{
			{"action_id": actionID, "value": value},
		},
		"response_url": "https://hooks.slack.com/actions/T001/B001/xxxxx",
		"trigger_id":   "trig-1",
	})
	require.NoError(t, err)
	form := url.Values{}
	form.Set("payload", string(inner))
	return form.Encode()
}

// withFrozenClock pins slack's package clock so the timestamp window matches
// the request's signed timestamp.
func withFrozenClock(t *testing.T, ts time.Time) {
	t.Helper()
	restore := slack.SetNowForTest(func() time.Time { return ts })
	t.Cleanup(restore)
}

func TestSlackInteractivity_ApprovedDispatches(t *testing.T) {
	const secret = "test-signing-secret"
	approvals := &fakeApprovals{}
	h := handler.SlackInteractivity(approvals, []byte(secret))

	body := buildPayload(t, "approve", "run-42:approved", "alice@acme.dev")
	ts := "1700000000"
	sig := signRequest(t, secret, ts, body)
	withFrozenClock(t, time.Unix(1_700_000_010, 0))

	req := httptest.NewRequest(http.MethodPost, "/v1/integrations/slack/interactivity", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()

	h(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	calls := approvals.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "run-42", calls[0].RunID)
	require.Equal(t, domain.ApprovalApproved, calls[0].Decision)
	require.Equal(t, "alice@acme.dev", calls[0].Email)
}

func TestSlackInteractivity_RejectsBadSignature(t *testing.T) {
	const secret = "test-signing-secret"
	approvals := &fakeApprovals{}
	h := handler.SlackInteractivity(approvals, []byte(secret))

	body := buildPayload(t, "approve", "run-1:approved", "alice@acme.dev")
	ts := "1700000000"
	// Sign with a different secret — the handler must reject.
	sig := signRequest(t, "wrong-secret", ts, body)
	withFrozenClock(t, time.Unix(1_700_000_010, 0))

	req := httptest.NewRequest(http.MethodPost, "/v1/integrations/slack/interactivity", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()

	h(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, approvals.snapshot(), "Decide must not be called on bad sig")
}

func TestSlackInteractivity_RejectsStaleTimestamp(t *testing.T) {
	const secret = "test-signing-secret"
	approvals := &fakeApprovals{}
	h := handler.SlackInteractivity(approvals, []byte(secret))

	body := buildPayload(t, "approve", "run-1:approved", "alice@acme.dev")
	ts := "1700000000"
	sig := signRequest(t, secret, ts, body)
	// 10 minutes in the future relative to the timestamp — outside the 5-minute window.
	withFrozenClock(t, time.Unix(1_700_000_600, 0))

	req := httptest.NewRequest(http.MethodPost, "/v1/integrations/slack/interactivity", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()

	h(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, approvals.snapshot())
}

func TestSlackInteractivity_RejectsBadValue(t *testing.T) {
	const secret = "test-signing-secret"
	approvals := &fakeApprovals{}
	h := handler.SlackInteractivity(approvals, []byte(secret))

	// value missing the colon → decoder rejects.
	body := buildPayload(t, "approve", "no-colon-here", "alice@acme.dev")
	ts := "1700000000"
	sig := signRequest(t, secret, ts, body)
	withFrozenClock(t, time.Unix(1_700_000_010, 0))

	req := httptest.NewRequest(http.MethodPost, "/v1/integrations/slack/interactivity", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()

	h(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, approvals.snapshot())
}

func TestSlackInteractivity_RejectsBadDecision(t *testing.T) {
	const secret = "test-signing-secret"
	approvals := &fakeApprovals{}
	h := handler.SlackInteractivity(approvals, []byte(secret))

	body := buildPayload(t, "approve", "run-1:maybe-later", "alice@acme.dev")
	ts := "1700000000"
	sig := signRequest(t, secret, ts, body)
	withFrozenClock(t, time.Unix(1_700_000_010, 0))

	req := httptest.NewRequest(http.MethodPost, "/v1/integrations/slack/interactivity", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()

	h(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, approvals.snapshot())
}

func TestSlackInteractivity_PropagatesApprovalError(t *testing.T) {
	const secret = "test-signing-secret"
	approvals := &fakeApprovals{failErr: errSentinel}
	h := handler.SlackInteractivity(approvals, []byte(secret))

	body := buildPayload(t, "reject", "run-7:rejected", "bob@acme.dev")
	ts := "1700000000"
	sig := signRequest(t, secret, ts, body)
	withFrozenClock(t, time.Unix(1_700_000_010, 0))

	req := httptest.NewRequest(http.MethodPost, "/v1/integrations/slack/interactivity", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()

	h(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

// errSentinel is the canned failure the propagation test injects into Decide.
var errSentinel = decideErr{msg: "approval service unavailable"}

type decideErr struct{ msg string }

func (e decideErr) Error() string { return e.msg }
