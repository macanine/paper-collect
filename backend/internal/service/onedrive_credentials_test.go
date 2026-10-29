package service

import (
	"context"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
)

func TestOneDriveRotatedTokenPreservesConcurrentCredentialChanges(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "rotation")
	id := newID()
	initial, err := svc.sealSecret(id, map[string]string{"refresh_token": "old", "client_secret": "client"})
	if err != nil {
		t.Fatal(err)
	}
	p := model.StorageProvider{ID: id, Name: "OneDrive", Kind: model.KindOneDrive, Role: model.RoleBackup, Enabled: true, ConfigJSON: `{"client_id":"app"}`, SecretCipher: initial, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := conn.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	var cfg storage.Config
	svc.factory = func(c storage.Config) (storage.Adapter, error) { cfg = c; return storage.New(c) }
	if _, err := svc.adapterFor(p); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveRefreshToken(context.Background(), "old", "new"); err != nil {
		t.Fatal(err)
	}
	var current model.StorageProvider
	if err := conn.First(&current, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := svc.box.Open(current.SecretCipher, []byte(id))
	if err != nil {
		t.Fatal(err)
	}
	secrets := model.DecodeMap(string(raw))
	if secrets["refresh_token"] != "new" || secrets["client_secret"] != "client" {
		t.Fatal("rotation did not preserve credentials")
	}
	changed, err := svc.sealSecret(id, map[string]string{"refresh_token": "manually-changed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Model(&model.StorageProvider{}).Where("id = ?", id).Update("secret_cipher", changed).Error; err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveRefreshToken(context.Background(), "new", "stale"); err != nil {
		t.Fatal(err)
	}
	if err := conn.First(&current, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if current.SecretCipher != changed {
		t.Fatal("rotation overwrote concurrently edited credentials")
	}
}

func TestOneDriveAuthorizationModeChangeRequiresNewToken(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "onedrive-mode")
	id := newID()
	cipher, err := svc.sealSecret(id, map[string]string{"refresh_token": "old"})
	if err != nil {
		t.Fatal(err)
	}
	provider := model.StorageProvider{ID: id, Name: "OneDrive", Kind: model.KindOneDrive, Role: model.RoleBackup, Enabled: true, Priority: 1, Weight: 1, ConfigJSON: `{"client_id":"app"}`, SecretCipher: cipher}
	if err := conn.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	cfg := map[string]string{"auth_mode": "openlist"}
	if _, err := svc.UpdateProvider(context.Background(), auth.Principal{UserID: "admin", Role: model.RoleAdmin}, id, ProviderPatch{Config: &cfg}); err == nil {
		t.Fatal("switching mode without a new token must be rejected")
	}
	old := map[string]string{"refresh_token": "old"}
	if _, err := svc.UpdateProvider(context.Background(), auth.Principal{UserID: "admin", Role: model.RoleAdmin}, id, ProviderPatch{Config: &cfg, Secret: &old}); err == nil {
		t.Fatal("switching mode with the old token must be rejected")
	}
	secret := map[string]string{"refresh_token": "new"}
	if _, err := svc.UpdateProvider(context.Background(), auth.Principal{UserID: "admin", Role: model.RoleAdmin}, id, ProviderPatch{Config: &cfg, Secret: &secret}); err != nil {
		t.Fatalf("switch with a new refresh token: %v", err)
	}
}
