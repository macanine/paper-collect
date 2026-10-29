package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
)

func TestDirectKeyLegacyCompatibilityAndCurrentOwnerState(t *testing.T) {
	s, db := newWorkerTestService(t, t.TempDir(), "key-auth")
	ctx := context.Background()
	user := model.User{ID: newID(), Username: "key_owner", PasswordHash: "unused", Role: model.RoleAdmin, Enabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	keyID := newID()
	secret := "legacy-test-random-secret-value"
	key := model.APIKey{ID: keyID, UserID: user.ID, Name: "legacy", SecretHash: auth.HashToken(secret), ScopesJSON: model.EncodeStrings([]string{model.ScopeMaterialsRead, model.ScopeStorageWrite})}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	credential := auth.LegacyAPIKeyPrefix + keyID + "." + secret
	p, err := s.Authenticate(ctx, credential)
	if err != nil || !p.IsAgent() || !p.Has(model.ScopeMaterialsRead) || !p.Has(model.ScopeStorageWrite) {
		t.Fatal("legacy key did not authenticate directly")
	}
	if _, err := s.Authenticate(ctx, credential+"wrong"); err == nil {
		t.Fatal("wrong legacy secret was accepted")
	}
	legacyJWT, err := s.signer.Sign(user.ID, "", keyID)
	if err != nil {
		t.Fatal(err)
	}
	if old, err := s.Authenticate(ctx, legacyJWT); err != nil || old.KeyID != keyID {
		t.Fatal("previously issued agent JWT should remain valid until expiry")
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("role", model.RoleViewer).Error; err != nil {
		t.Fatal(err)
	}
	p, err = s.Authenticate(ctx, credential)
	if err != nil || !p.Has(model.ScopeMaterialsRead) || p.Has(model.ScopeStorageWrite) {
		t.Fatal("key permissions did not follow current owner role")
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, credential); err == nil {
		t.Fatal("disabled owner key was accepted")
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.APIKey{}).Where("id = ?", keyID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, credential); err == nil {
		t.Fatal("expired key was accepted")
	}
	if _, err := s.Authenticate(ctx, legacyJWT); err == nil {
		t.Fatal("expired key must invalidate its old JWT")
	}
}

func TestNewOpaqueKeyUsesIndexedSecretHash(t *testing.T) {
	s, db := newWorkerTestService(t, t.TempDir(), "key-auth")
	user := model.User{ID: newID(), Username: "key_owner", PasswordHash: "unused", Role: model.RoleAdmin, Enabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	key, secret, err := s.CreateKey(context.Background(), auth.Principal{UserID: user.ID, Role: user.Role}, "test", []string{model.ScopeJobsRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "sk-") || strings.Contains(secret, key.ID) || key.ExpiresAt != nil {
		t.Fatal("new key must be opaque and non-expiring")
	}
	if !db.Migrator().HasIndex(&model.APIKey{}, "idx_api_keys_secret_hash") {
		t.Fatal("API key hash index missing")
	}
	var stored model.APIKey
	if err := db.First(&stored, "id = ?", key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SecretHash == secret || strings.Contains(stored.SecretHash, secret) {
		t.Fatal("key stored in plaintext")
	}
	p, err := s.Authenticate(context.Background(), secret)
	if err != nil || p.KeyID != key.ID || !p.Has(model.ScopeJobsRead) {
		t.Fatal("new opaque key did not authenticate")
	}
	if err := db.Model(&model.APIKey{}).Where("id = ?", key.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(context.Background(), secret); err == nil {
		t.Fatal("expired opaque key was accepted")
	}
}
