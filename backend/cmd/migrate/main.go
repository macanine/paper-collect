// Command migrate creates the schema and can copy an existing SQLite database
// into an empty PostgreSQL database before application instances start.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/nic/paper-collect/backend/internal/config"
	"github.com/nic/paper-collect/backend/internal/db"
	"github.com/nic/paper-collect/backend/internal/model"
)

func main() {
	source := flag.String("from-sqlite", "", "copy an existing SQLite database into PostgreSQL")
	flag.Parse()
	if err := run(*source); err != nil {
		log.Fatalf("迁移失败: %v", err)
	}
}

func run(source string) error {
	cfg, err := config.Load(func(key string) string {
		if key == "ADMIN_USERNAME" || key == "ADMIN_PASSWORD" {
			return ""
		}
		return os.Getenv(key)
	})
	if err != nil {
		return err
	}
	if cfg.DatabaseDriver != "postgres" {
		return errors.New("migrate 只用于 PostgreSQL 目标库")
	}
	target, err := db.Open("postgres", cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	if err := db.Migrate(target); err != nil {
		return err
	}
	if source == "" {
		return nil
	}

	var users int64
	if err := target.Model(&model.User{}).Count(&users).Error; err != nil {
		return fmt.Errorf("检查目标库: %w", err)
	}
	if users != 0 {
		log.Print("目标 PostgreSQL 已有数据，跳过 SQLite 导入")
		return nil
	}
	origin, err := db.Open("sqlite", source)
	if err != nil {
		return err
	}
	return db.CopyModels(origin, target)
}
