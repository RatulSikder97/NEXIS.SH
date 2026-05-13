package slack_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
)

// signSlack builds an X-Slack-Signature value for the supplied body +
// timestamp + secret using the same formula Slack documents:
// "v0=" + hex(hmac-sha256(secret, "v0:<ts>:<body>")).
func signSlack(t *testing.T, secret, ts, body string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("v0:" + ts + ":" + body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyRequestSignature_AcceptsValid(t *testing.T) {
	const secret = "test-signing-secret"
	const body = `payload=%7B%22type%22%3A%22block_actions%22%7D`
	ts := "1700000000"
	sig := signSlack(t, secret, ts, body)

	// Pin the clock so the timestamp is fresh.
	restore := slack.SetNowForTest(func() time.Time { return time.Unix(1_700_000_010, 0) })
	t.Cleanup(restore)

	require.NoError(t, slack.VerifyRequestSignature(secret, body, ts, sig))
}

func TestVerifyRequestSignature_RejectsOldTimestamp(t *testing.T) {
	const secret = "test-signing-secret"
	const body = "payload=anything"
	ts := "1700000000" // 10 minutes before our pinned "now"
	sig := signSlack(t, secret, ts, body)

	restore := slack.SetNowForTest(func() time.Time { return time.Unix(1_700_000_600, 0) })
	t.Cleanup(restore)

	err := slack.VerifyRequestSignature(secret, body, ts, sig)
	require.True(t, errors.Is(err, slack.ErrInvalidSignature),
		"want ErrInvalidSignature, got %v", err)
}

func TestVerifyRequestSignature_RejectsFutureTimestamp(t *testing.T) {
	const secret = "test-signing-secret"
	const body = "payload=anything"
	ts := "1700000600" // 10 minutes after pinned now
	sig := signSlack(t, secret, ts, body)

	restore := slack.SetNowForTest(func() time.Time { return time.Unix(1_700_000_000, 0) })
	t.Cleanup(restore)

	err := slack.VerifyRequestSignature(secret, body, ts, sig)
	require.True(t, errors.Is(err, slack.ErrInvalidSignature),
		"want ErrInvalidSignature, got %v", err)
}

func TestVerifyRequestSignature_RejectsMismatch(t *testing.T) {
	const secret = "test-signing-secret"
	const body = "payload=tampered"
	ts := "1700000010"
	sig := signSlack(t, "WRONG-SECRET", ts, body) // signed with wrong secret

	restore := slack.SetNowForTest(func() time.Time { return time.Unix(1_700_000_020, 0) })
	t.Cleanup(restore)

	err := slack.VerifyRequestSignature(secret, body, ts, sig)
	require.True(t, errors.Is(err, slack.ErrInvalidSignature),
		"want ErrInvalidSignature, got %v", err)
}

func TestVerifyRequestSignature_RejectsMissingHeaders(t *testing.T) {
	const secret = "test-signing-secret"
	err := slack.VerifyRequestSignature(secret, "body", "", "")
	require.True(t, errors.Is(err, slack.ErrInvalidSignature))
}

func TestVerifyRequestSignature_RejectsBadTimestamp(t *testing.T) {
	const secret = "test-signing-secret"
	err := slack.VerifyRequestSignature(secret, "body", "not-a-number", "v0=deadbeef")
	require.True(t, errors.Is(err, slack.ErrInvalidSignature))
}

func TestParseInteractivity_BlockActions(t *testing.T) {
	payload := `{
		"type": "block_actions",
		"user": {"id": "U001", "username": "alice", "email": "alice@acme.dev"},
		"team": {"id": "T001"},
		"actions": [
			{"action_id": "approve", "value": "run-42:approved"}
		],
		"response_url": "https://hooks.slack.com/actions/T001/B001/xxxxx",
		"trigger_id": "trig-1"
	}`
	form := url.Values{}
	form.Set("payload", payload)

	got, err := slack.ParseInteractivity(form)
	require.NoError(t, err)
	require.Equal(t, "block_actions", got.Type)
	require.Equal(t, "U001", got.User.ID)
	require.Equal(t, "alice", got.User.Username)
	require.Equal(t, "alice@acme.dev", got.User.Email)
	require.Equal(t, "T001", got.Team.ID)
	require.Len(t, got.Actions, 1)
	require.Equal(t, "approve", got.Actions[0].ActionID)
	require.Equal(t, "run-42:approved", got.Actions[0].Value)
	require.Equal(t, "trig-1", got.TriggerID)
	require.Contains(t, got.ResponseURL, "hooks.slack.com")
}

func TestParseInteractivity_RejectsMissingPayload(t *testing.T) {
	_, err := slack.ParseInteractivity(url.Values{})
	require.ErrorContains(t, err, "missing payload")
}

func TestParseInteractivity_RejectsUnsupportedType(t *testing.T) {
	form := url.Values{}
	form.Set("payload", `{"type": "shortcut"}`)
	_, err := slack.ParseInteractivity(form)
	require.ErrorContains(t, err, "unsupported")
}

func TestParseInteractivity_RejectsMalformedJSON(t *testing.T) {
	form := url.Values{}
	form.Set("payload", `{this-is-not-json}`)
	_, err := slack.ParseInteractivity(form)
	require.ErrorContains(t, err, "decode payload")
}
