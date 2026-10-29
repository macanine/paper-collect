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

func TestMigrateAddsProgressToExistingJobs(t *testing.T) {
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := conn.Exec(`CREATE TABLE jobs (id text PRIMARY KEY, kind text NOT NULL, status text NOT NULL, upload_id text, replica_id text, material_id text, attempts integer NOT NULL, error text NOT NULL, lease_owner text, lease_until datetime, heartbeat_at datetime, created_at datetime, updated_at datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec(`INSERT INTO jobs (id,kind,status,attempts,error) VALUES ('old','ingest','pending',0,'')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	var job model.Job
	if err := conn.First(&job, "id = ?", "old").Error; err != nil {
		t.Fatal(err)
	}
	if job.ProgressMessage != "" || job.LogJSON != "" || job.ProgressTotal != 0 {
		t.Fatal("legacy job was not preserved")
	}
}

func TestMigrateAddsKeyLookupIndexWithoutChangingKeys(t *testing.T) {
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	if err := conn.Migrator().DropIndex(&model.APIKey{}, "idx_api_keys_secret_hash"); err != nil {
		t.Fatal(err)
	}
	key := model.APIKey{ID: "old-key", UserID: "owner", Name: "old", SecretHash: "legacy-hash", ScopesJSON: `["materials:read"]`}
	if err := conn.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	var saved model.APIKey
	if err := conn.First(&saved, "id = ?", key.ID).Error; err != nil || saved.SecretHash != key.SecretHash || !conn.Migrator().HasIndex(&model.APIKey{}, "idx_api_keys_secret_hash") {
		t.Fatal("migration did not preserve the legacy key and add its lookup index")
	}
}
