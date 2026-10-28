// Package db opens the GORM connection for SQLite (pure Go driver) or PostgreSQL and runs migrations.
package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open connects to the database. Logging is silent so SQL parameters (which may
// carry secret ciphertext or hashes) never reach process logs.
func Open(driver, dsn string) (*gorm.DB, error) {
	cfg := &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	}
	var (
		conn *gorm.DB
		err  error
	)
	switch driver {
	case "sqlite":
		fileDSN, err := sqliteDSN(dsn)
		if err != nil {
			return nil, err
		}
		conn, err = gorm.Open(sqlite.Open(fileDSN), cfg)
		if err != nil {
			return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
		}
		sqlDB, err := conn.DB()
		if err != nil {
			return nil, err
		}
		// SQLite is a single-writer store; one connection avoids lock contention.
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetConnMaxLifetime(0)
	case "postgres":
		conn, err = gorm.Open(postgres.Open(dsn), cfg)
		if err != nil {
			return nil, fmt.Errorf("打开 PostgreSQL 失败（DSN 已隐藏）: %w", err)
		}
		sqlDB, err := conn.DB()
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(10)
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %s", driver)
	}
	return conn, nil
}

// sqliteDSN turns a file path or file: URI into a DSN with the pragmas the app depends on.
func sqliteDSN(dsn string) (string, error) {
	path := dsn
	query := ""
	if strings.HasPrefix(dsn, "file:") {
		rest := strings.TrimPrefix(dsn, "file:")
		if i := strings.Index(rest, "?"); i >= 0 {
			path, query = rest[:i], rest[i+1:]
		} else {
			path = rest
		}
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", fmt.Errorf("创建 SQLite 目录失败: %w", err)
		}
	}
	params := []string{
		"_pragma=foreign_keys(1)",
		"_pragma=busy_timeout(5000)",
		"_pragma=journal_mode(WAL)",
		"_txlock=immediate",
	}
	if query != "" {
		params = append(params, query)
	}
	return "file:" + path + "?" + strings.Join(params, "&"), nil
}

// Migrate creates or updates all tables. It is safe to run on every start.
func Migrate(conn *gorm.DB) error {
	if err := conn.AutoMigrate(model.Models()...); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	return nil
}
