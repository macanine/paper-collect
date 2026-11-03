package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func configEnv(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"DATA_DIR":       t.TempDir(),
		"JWT_SECRET":     strings.Repeat("s", 32),
		"MASTER_KEY":     base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"PULL_CACHE_TTL": "",
	}
}

func loadTestConfig(t *testing.T, values map[string]string) (Config, error) {
	t.Helper()
	return Load(func(name string) string { return values[name] })
}

func TestPullCacheTTL(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		cfg, err := loadTestConfig(t, configEnv(t))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PullCacheTTL != 15*time.Minute {
			t.Fatalf("PullCacheTTL = %s, want 15m", cfg.PullCacheTTL)
		}
	})

	t.Run("configured", func(t *testing.T) {
		values := configEnv(t)
		values["PULL_CACHE_TTL"] = "45m"
		cfg, err := loadTestConfig(t, values)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PullCacheTTL != 45*time.Minute {
			t.Fatalf("PullCacheTTL = %s, want 45m", cfg.PullCacheTTL)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		values := configEnv(t)
		values["PULL_CACHE_TTL"] = "0s"
		_, err := loadTestConfig(t, values)
		if err == nil || !strings.Contains(err.Error(), "PULL_CACHE_TTL") {
			t.Fatalf("expected PULL_CACHE_TTL error, got %v", err)
		}
	})
}
