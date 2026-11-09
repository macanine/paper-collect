package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
)

// API keys are opaque 256-bit secrets.
const APIKeyPrefix = "sk-"

// NewRefreshToken returns an opaque refresh token and its SHA-256 hash.
// Only the hash is stored on the server.
func NewRefreshToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken hashes an opaque token (refresh token or API key secret) for storage and lookup.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewAPIKey returns a new internal id, an opaque secret shown once, and its hash.
func NewAPIKey() (id, full, secretHash string, err error) {
	id = uuid.NewString()
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(b)
	full = APIKeyPrefix + secret
	return id, full, HashToken(secret), nil
}

// ErrInvalidAPIKey is returned for any malformed API key. It carries no detail.
var ErrInvalidAPIKey = errors.New("auth: invalid api key")

// ParseAPIKey accepts opaque keys. The secret is looked up by its hash.
func ParseAPIKey(full string) (id, secret string, err error) {
	if len(full) <= len(APIKeyPrefix) || full[:len(APIKeyPrefix)] != APIKeyPrefix {
		return "", "", ErrInvalidAPIKey
	}
	secret = full[len(APIKeyPrefix):]
	if len(secret) != base64.RawURLEncoding.EncodedLen(32) {
		return "", "", ErrInvalidAPIKey
	}
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(secret)
	if decodeErr != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != secret {
		return "", "", ErrInvalidAPIKey
	}
	return "", secret, nil
}
