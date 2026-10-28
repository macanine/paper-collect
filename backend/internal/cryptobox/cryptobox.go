// Package cryptobox encrypts provider secrets with AES-256-GCM under MASTER_KEY.
package cryptobox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Box seals and opens small secrets. The AAD binds a ciphertext to its owner.
type Box struct {
	aead cipher.AEAD
}

// New creates a Box from a 32-byte master key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("cryptobox: master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext and returns base64(nonce||ciphertext).
func (b *Box) Seal(plaintext, aad []byte) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, plaintext, aad)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// ErrOpen is returned when authentication fails. It intentionally carries no detail.
var ErrOpen = errors.New("cryptobox: cannot open ciphertext")

// Open reverses Seal. It fails when the ciphertext or AAD was altered.
func (b *Box) Open(encoded string, aad []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrOpen
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return nil, ErrOpen
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}
