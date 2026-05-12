package local

import (
	"bytes"
	"image/png"

	"github.com/pquerna/otp/totp"
)

// generateMFASecret creates a fresh TOTP key for the given account and
// returns the secret (base32) and a PNG QR code that encodes the
// otpauth:// URI for the authenticator app.
func generateMFASecret(account string) (secret string, qrPNG []byte, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "NEXIS", AccountName: account})
	if err != nil {
		return "", nil, err
	}
	img, err := key.Image(200, 200)
	if err != nil {
		return "", nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", nil, err
	}
	return key.Secret(), buf.Bytes(), nil
}

func verifyTOTP(secret, code string) bool {
	return totp.Validate(code, secret)
}
