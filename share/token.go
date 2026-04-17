package share

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
)

func LoadOrCreateSecret(path string) ([]byte, error) {
	secret, err := os.ReadFile(path)
	if err == nil && len(secret) > 0 {
		return secret, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read secret: %w", err)
	}

	secret = make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}

	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, fmt.Errorf("write secret: %w", err)
	}

	return secret, nil
}

func GenerateShareID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

const DefaultTokenBytes = 8

func ShareToken(secret []byte, shareID string, tokenBytes int) string {
	if tokenBytes <= 0 {
		tokenBytes = DefaultTokenBytes
	}
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte(shareID))
	sum := h.Sum(nil)
	if tokenBytes > len(sum) {
		tokenBytes = len(sum)
	}
	return base64.RawURLEncoding.EncodeToString(sum[:tokenBytes])
}

func ValidateShareToken(secret []byte, shareID string, token string, tokenBytes int) bool {
	expected := ShareToken(secret, shareID, tokenBytes)
	return hmac.Equal([]byte(expected), []byte(token))
}
