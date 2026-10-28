// Package config loads and validates the process configuration from environment variables.
// Validation errors name the variable but never echo its value.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Config is the validated runtime configuration.
type Config struct {
	Addr           string
	DataDir        string
	DatabaseDriver string // sqlite | postgres
	DatabaseDSN    string
	AutoMigrate    bool
	WebDist        string // absolute path; the directory may be absent
	AdminUsername  string // optional; only needed to bootstrap an empty database
	AdminPassword  string // optional; only needed to bootstrap an empty database
	JWTSecret      string
	MasterKey      []byte // 32 bytes, decoded from base64
	Worker         WorkerConfig
}

// WorkerConfig controls the embedded job runners. Analyze and index tasks are
// isolated by material; ingest and replicate have their own bounded pools.
type WorkerConfig struct {
	InstanceID           string
	IngestConcurrency    int
	ReplicateConcurrency int
	AnalyzeConcurrency   int
	IndexConcurrency     int
	LeaseDuration        time.Duration
	HeartbeatInterval    time.Duration
	PollInterval         time.Duration
}

var adminUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)

// Load reads configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	cfg := Config{}

	cfg.Addr = valueOr(getenv("ADDR"), ":8080")
	if _, _, err := net.SplitHostPort(cfg.Addr); err != nil {
		add("ADDR 必须是 host:port 形式（例如 :8080）")
	}

	dataDir := valueOr(getenv("DATA_DIR"), "./data")
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		add("DATA_DIR 无法解析为绝对路径")
	}
	cfg.DataDir = absData

	cfg.DatabaseDriver = strings.ToLower(valueOr(getenv("DATABASE_DRIVER"), "sqlite"))
	switch cfg.DatabaseDriver {
	case "sqlite":
		dsn := strings.TrimSpace(getenv("DATABASE_DSN"))
		if dsn == "" {
			dsn = filepath.Join(cfg.DataDir, "paper.db")
		}
		cfg.DatabaseDSN = dsn
	case "postgres":
		dsn := strings.TrimSpace(getenv("DATABASE_DSN"))
		if dsn == "" {
			add("DATABASE_DRIVER=postgres 时必须设置 DATABASE_DSN（内容已隐藏）")
		}
		cfg.DatabaseDSN = dsn
	default:
		add("DATABASE_DRIVER 只能是 sqlite 或 postgres")
	}
	cfg.AutoMigrate = strings.ToLower(strings.TrimSpace(getenv("DB_AUTO_MIGRATE"))) != "false"

	webDist, err := filepath.Abs(valueOr(getenv("WEB_DIST"), "../web/dist"))
	if err != nil {
		add("WEB_DIST 无法解析为绝对路径")
	}
	cfg.WebDist = webDist

	cfg.AdminUsername = strings.TrimSpace(getenv("ADMIN_USERNAME"))
	cfg.AdminPassword = getenv("ADMIN_PASSWORD")
	if cfg.AdminUsername != "" && !adminUsernamePattern.MatchString(cfg.AdminUsername) {
		add("ADMIN_USERNAME 只允许 3-64 位字母、数字、下划线或连字符")
	}
	if cfg.AdminPassword != "" && utf8.RuneCountInString(cfg.AdminPassword) < 12 {
		add("ADMIN_PASSWORD 至少 12 个字符（值已隐藏）")
	}
	if (cfg.AdminUsername == "") != (cfg.AdminPassword == "") {
		add("ADMIN_USERNAME 与 ADMIN_PASSWORD 必须同时设置，或同时留空")
	}

	cfg.JWTSecret = getenv("JWT_SECRET")
	if len(cfg.JWTSecret) < 32 {
		add("JWT_SECRET 缺失或少于 32 个字符（值已隐藏）")
	}

	masterRaw := strings.TrimSpace(getenv("MASTER_KEY"))
	if masterRaw == "" {
		add("MASTER_KEY 缺失：需要 32 字节的 base64 值（值已隐藏）")
	} else {
		key, err := decodeBase64(masterRaw)
		if err != nil || len(key) != 32 {
			add("MASTER_KEY 必须是解码后恰好 32 字节的 base64（值已隐藏）")
		} else {
			cfg.MasterKey = key
		}
	}

	cfg.Worker = WorkerConfig{
		InstanceID:           strings.TrimSpace(getenv("WORKER_INSTANCE_ID")),
		IngestConcurrency:    positiveInt(getenv, "WORKER_INGEST_CONCURRENCY", 1, &problems),
		ReplicateConcurrency: positiveInt(getenv, "WORKER_REPLICATE_CONCURRENCY", 2, &problems),
		AnalyzeConcurrency:   positiveInt(getenv, "WORKER_ANALYZE_CONCURRENCY", 2, &problems),
		IndexConcurrency:     positiveInt(getenv, "WORKER_INDEX_CONCURRENCY", 2, &problems),
		LeaseDuration:        positiveDuration(getenv, "WORKER_LEASE_DURATION", 2*time.Minute, &problems),
		HeartbeatInterval:    positiveDuration(getenv, "WORKER_HEARTBEAT_INTERVAL", 20*time.Second, &problems),
		PollInterval:         positiveDuration(getenv, "WORKER_POLL_INTERVAL", 2*time.Second, &problems),
	}
	if cfg.Worker.HeartbeatInterval >= cfg.Worker.LeaseDuration {
		add("WORKER_HEARTBEAT_INTERVAL 必须小于 WORKER_LEASE_DURATION")
	}
	if cfg.DatabaseDriver == "sqlite" && strings.TrimSpace(cfg.Worker.InstanceID) != "" {
		add("SQLite 不支持多实例，WORKER_INSTANCE_ID 仅用于 PostgreSQL")
	}

	if len(problems) > 0 {
		errs := make([]error, 0, len(problems))
		for _, p := range problems {
			errs = append(errs, errors.New(p))
		}
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func positiveInt(getenv func(string) string, name string, fallback int, problems *[]string) int {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 64 {
		*problems = append(*problems, fmt.Sprintf("%s 必须是 1 到 64 的整数", name))
		return fallback
	}
	return n
}

func positiveDuration(getenv func(string) string, name string, fallback time.Duration, problems *[]string) time.Duration {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		*problems = append(*problems, fmt.Sprintf("%s 必须是正的 Go duration（例如 30s）", name))
		return fallback
	}
	return d
}

func valueOr(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func decodeBase64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
