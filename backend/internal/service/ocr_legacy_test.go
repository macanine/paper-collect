package service

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/cryptobox"
	"github.com/nic/paper-collect/backend/internal/db"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
)

func TestLegacyMinerUProviderMigrationRebindsCipher(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	store, err := blob.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	box, err := cryptobox.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := box.Seal([]byte("legacy-mineru-token"), []byte("ai.settings"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := conn.Create(&model.AISettings{
		ID: 1, MinerUBaseURL: "https://mineru.net", MinerUTokenCipher: cipher,
		MaxPages: 3, MaxImages: 2, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	signer, err := auth.NewSigner(strings.Repeat("s", 32), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Options{
		DB: conn, Blobs: store, Box: box, Signer: signer,
		Limiter: auth.NewLimiter(time.Now, 10, time.Minute, time.Minute),
		Factory: storage.New, Logf: t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := svc.GetAISettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Settings.OCRProviderID == "" || !view.OCRConfigured {
		t.Fatalf("legacy provider not selected: %+v", view)
	}
	var provider model.OCRProvider
	if err := conn.First(&provider, "id = ?", view.Settings.OCRProviderID).Error; err != nil {
		t.Fatal(err)
	}
	token, err := box.Open(provider.TokenCipher, []byte(provider.ID))
	if err != nil || string(token) != "legacy-mineru-token" {
		t.Fatalf("migrated token cannot be opened: %q %v", token, err)
	}
}
