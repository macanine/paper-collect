// Package config loads and validates the process configuration from environment variables.
// Validation errors name the variable but never echo its value.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Roles of a process.
const (
	RoleMaster = "master"
	RoleWorker = "worker"
)

// RemoteKinds are the job kinds a worker node can execute.
var RemoteKinds = []string{"replicate", "analyze", "index"}

// Config is the validated runtime configuration.
type Config struct {
	Role           string // master | worker
	NodeName       string // display name of the master's own node
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
	PullCacheTTL   time.Duration
	Worker         WorkerConfig
	Node           NodeConfig // ROLE=worker only
}

// NodeConfig configures a worker node. A worker has no database, no
// JWT_SECRET and no MASTER_KEY; credentials arrive with each job.
type NodeConfig struct {
	MasterURL string
	Token     string
	CacheDir  string
	CacheMax  int64
	Slots     map[string]int
	// RapidOCRURL replaces a self-hosted OCR address the node cannot reach.
	RapidOCRURL string
}

// WorkerConfig controls the embedded job runners. Analyze and index tasks are
// isolated by material; ingest and replicate have their own bounded pools.
type WorkerConfig struct {
	// Kinds lists the remote-capable kinds the master also runs itself.
	Kinds                []string
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

	cfg := Config{Role: strings.ToLower(valueOr(getenv("ROLE"), RoleMaster))}
	switch cfg.Role {
	case RoleWorker:
		return loadWorker(getenv)
	case RoleMaster:
	default:
		return Config{}, errors.New("ROLE 只能是 master 或 worker")
	}
	cfg.NodeName = valueOr(getenv("NODE_NAME"), "主端")
	if utf8.RuneCountInString(cfg.NodeName) > 64 {
		add("NODE_NAME 不能超过 64 个字符")
	}

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
	cfg.PullCacheTTL = positiveDuration(getenv, "PULL_CACHE_TTL", 15*time.Minute, &problems)

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
		Kinds:                masterKinds(getenv("WORKER_KINDS"), &problems),
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
	// Worker nodes renew their leases on a fixed 10 second heartbeat.
	if cfg.Worker.LeaseDuration < 30*time.Second {
		add("WORKER_LEASE_DURATION 不能小于 30s，否则从端的任务会在心跳之间失去租约")
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

// masterKinds parses WORKER_KINDS. Empty means all remote kinds; "none" means
// the master leaves them to worker nodes.
func masterKinds(raw string, problems *[]string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return append([]string{}, RemoteKinds...)
	}
	if raw == "none" {
		return []string{}
	}
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		kind := strings.TrimSpace(part)
		if !isRemoteKind(kind) {
			*problems = append(*problems, "WORKER_KINDS 只能包含 replicate、analyze、index，或为 none")
			return append([]string{}, RemoteKinds...)
		}
		out = append(out, kind)
	}
	return out
}

func isRemoteKind(kind string) bool {
	for _, k := range RemoteKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func loadWorker(getenv func(string) string) (Config, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	cfg := Config{Role: RoleWorker}
	node := NodeConfig{Token: strings.TrimSpace(getenv("NODE_TOKEN"))}
	raw := strings.TrimRight(strings.TrimSpace(getenv("MASTER_URL")), "/")
	u, err := url.Parse(raw)
	switch {
	case raw == "" || err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https"):
		add("ROLE=worker 时 MASTER_URL 必须是 http(s) 地址")
	case u.Scheme == "http" && !isLoopback(u.Hostname()):
		// Jobs carry decrypted credentials; only loopback may skip TLS.
		add("MASTER_URL 指向非本机地址时必须使用 https")
	}
	node.MasterURL = raw
	if !strings.HasPrefix(node.Token, "nd-") {
		add("ROLE=worker 时必须设置 NODE_TOKEN（在主端「节点」页面创建，值已隐藏）")
	}
	cacheDir, err := filepath.Abs(valueOr(getenv("CACHE_DIR"), "./cache"))
	if err != nil {
		add("CACHE_DIR 无法解析为绝对路径")
	}
	node.CacheDir = cacheDir
	node.CacheMax = 20 << 30
	if v := strings.TrimSpace(getenv("CACHE_MAX_BYTES")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			add("CACHE_MAX_BYTES 必须是非负整数（字节）")
		} else {
			node.CacheMax = n
		}
	}
	if v := strings.TrimRight(strings.TrimSpace(getenv("RAPIDOCR_URL")), "/"); v != "" {
		if u, err := url.Parse(v); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			add("RAPIDOCR_URL 必须是 http(s) 地址")
		}
		node.RapidOCRURL = v
	}
	node.Slots = map[string]int{"replicate": 2, "analyze": 1, "index": 1}
	if v := strings.TrimSpace(getenv("WORKER_SLOTS")); v != "" {
		node.Slots = map[string]int{}
		for _, part := range strings.Split(v, ",") {
			kind, count, ok := strings.Cut(strings.TrimSpace(part), "=")
			n, err := strconv.Atoi(strings.TrimSpace(count))
			if !ok || err != nil || n < 0 || n > 64 || !isRemoteKind(strings.TrimSpace(kind)) {
				add("WORKER_SLOTS 格式为 replicate=2,analyze=1,index=1，数量 0 到 64")
				break
			}
			if n > 0 {
				node.Slots[strings.TrimSpace(kind)] = n
			}
		}
		if len(node.Slots) == 0 {
			add("WORKER_SLOTS 至少需要一种任务类型")
		}
	}
	cfg.Node = node
	if len(problems) > 0 {
		errs := make([]error, 0, len(problems))
		for _, p := range problems {
			errs = append(errs, errors.New(p))
		}
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
