// Package clerk verifies Clerk webhook signatures (Svix HMAC-SHA256).
package clerk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// VerifyWebhook validates the Svix signature on a Clerk webhook request body.
// secret is the Clerk-provided webhook signing secret (whsec_…).
func VerifyWebhook(secret string, headers http.Header, body []byte) error {
	if secret == "" {
		return errors.New("clerk: webhook secret not configured")
	}
	id := headers.Get("svix-id")
	ts := headers.Get("svix-timestamp")
	sigs := headers.Get("svix-signature")
	if id == "" || ts == "" || sigs == "" {
		return errors.New("clerk: missing svix headers")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errors.New("clerk: invalid svix-timestamp")
	}
	if time.Since(time.Unix(tsInt, 0)).Abs() > 5*time.Minute {
		return errors.New("clerk: svix-timestamp outside 5 min window")
	}

	// secret format: whsec_<base64>
	secretRaw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		return errors.New("clerk: invalid webhook secret format")
	}

	signed := id + "." + ts + "." + string(body)
	mac := hmac.New(sha256.New, secretRaw)
	mac.Write([]byte(signed))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for _, sig := range strings.Split(sigs, " ") {
		// each sig is "v1,<base64>"
		parts := strings.SplitN(sig, ",", 2)
		if len(parts) != 2 {
			continue
		}
		if hmac.Equal([]byte(parts[1]), []byte(expected)) {
			return nil
		}
	}
	return errors.New("clerk: signature verification failed")
}
