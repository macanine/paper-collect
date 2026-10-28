package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Issuer and Audience are fixed; tokens with other values are rejected.
	Issuer   = "paper-collect"
	Audience = "paper-collect-api"
	// AccessTTL is the lifetime of access JWTs (browser and agent).
	AccessTTL = 15 * time.Minute
	// RefreshTTL bounds a browser session, counted from login; rotation does not extend it.
	RefreshTTL = 7 * 24 * time.Hour
	// MinSecretLength is the minimum JWT signing secret length.
	MinSecretLength = 32
)

// AccessClaims are the claims of access tokens. SessionID is set for browser
// tokens; KeyID identifies the API key backing an agent token and is rechecked
// against its current revocation, expiry and user state on every request.
type AccessClaims struct {
	SessionID string `json:"sid,omitempty"`
	KeyID     string `json:"key_id,omitempty"`
	jwt.RegisteredClaims
}

// Issuer signs and verifies access JWTs with HS256 only.
type Signer struct {
	secret []byte
	now    func() time.Time
}

// NewSigner creates a signer. The secret must be at least MinSecretLength bytes.
func NewSigner(secret string, now func() time.Time) (*Signer, error) {
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("auth: JWT secret must be at least %d bytes", MinSecretLength)
	}
	if now == nil {
		now = time.Now
	}
	return &Signer{secret: []byte(secret), now: now}, nil
}

// Sign issues an access token for userID. sessionID or keyID selects the token kind.
func (s *Signer) Sign(userID, sessionID, keyID string) (string, error) {
	now := s.now().UTC()
	jti, err := randomID()
	if err != nil {
		return "", err
	}
	claims := AccessClaims{
		SessionID: sessionID,
		KeyID:     keyID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   userID,
			Audience:  jwt.ClaimStrings{Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL)),
			ID:        jti,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// Parse verifies signature (HS256 only), issuer, audience, expiry and not-before.
func (s *Signer) Parse(raw string) (*AccessClaims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return s.now().UTC() }),
	)
	claims := &AccessClaims{}
	tok, err := parser.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return s.secret, nil
	})
	if err != nil || !tok.Valid {
		return nil, fmt.Errorf("auth: invalid access token")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("auth: token without subject")
	}
	return claims, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
