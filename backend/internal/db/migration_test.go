package db_test

import (
	"path/filepath"
	"testing"

	"github.com/nic/paper-collect/backend/internal/db"
	"github.com/nic/paper-collect/backend/internal/model"
)

func TestMigrateCurrentSchemaIsIdempotent(t *testing.T) {
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	if err := db.Migrate(conn); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	for _, table := range model.Models() {
		if !conn.Migrator().HasTable(table) {
			t.Fatalf("missing table for %T", table)
		}
	}
	if conn.Migrator().HasTable("ai_inspections") || conn.Migrator().HasColumn(&model.Job{}, "inspect_id") || conn.Migrator().HasColumn(&model.UploadEntry{}, "inspection_id") {
		t.Fatal("fresh schema contains removed inspection data")
	}

	user := model.User{ID: "user", Username: "admin", PasswordHash: "test-hash", Role: "admin", Enabled: true}
	provider := model.StorageProvider{ID: "provider", Name: "local", Kind: "local", Role: "primary", Enabled: true, ConfigJSON: "{}"}
	if err := conn.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var savedUser model.User
	if err := conn.First(&savedUser, "id = ?", user.ID).Error; err != nil || savedUser.Username != user.Username || savedUser.PasswordHash != user.PasswordHash || !savedUser.Enabled {
		t.Fatalf("account not preserved: %v", err)
	}
	var savedProvider model.StorageProvider
	if err := conn.First(&savedProvider, "id = ?", provider.ID).Error; err != nil || savedProvider.Name != provider.Name || savedProvider.ConfigJSON != provider.ConfigJSON || !savedProvider.Enabled {
		t.Fatalf("storage settings not preserved: %v", err)
	}
}
