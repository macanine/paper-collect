// Package auth holds credential primitives: password hashing, JWT signing and
// verification, opaque token generation, API key parsing, login throttling and
// the role-to-scope policy. Database lookups live in the service package.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB, OWASP baseline for argon2id
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// MinPasswordLength is the minimum length (in runes) for administrator and user passwords.
const MinPasswordLength = 12

// MaxPasswordBytes bounds input size for hashing.
const MaxPasswordBytes = 1024

// HashPassword returns an encoded argon2id hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks password against an encoded argon2id hash in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// DummyVerify spends comparable time when the user does not exist, to reduce timing signals.
func DummyVerify(password string) {
	_ = VerifyPassword(password, dummyHash)
}

var dummyHash = func() string {
	h, err := HashPassword("dummy-password-for-timing")
	if err != nil {
		panic(errors.New("auth: cannot initialise dummy hash"))
	}
	return h
}()
