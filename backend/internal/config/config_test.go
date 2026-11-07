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

func TestWorkerRoleNeedsNoSecretsButRejectsPlainRemoteHTTP(t *testing.T) {
	env := map[string]string{"ROLE": "worker", "NODE_TOKEN": "nd-" + strings.Repeat("a", 43), "CACHE_DIR": t.TempDir()}
	env["MASTER_URL"] = "https://paper.example.com/"
	cfg, err := loadTestConfig(t, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Node.MasterURL != "https://paper.example.com" || cfg.Node.Slots["replicate"] != 2 {
		t.Fatalf("unexpected worker config: %+v", cfg.Node)
	}
	env["MASTER_URL"] = "http://127.0.0.1:8080"
	env["WORKER_SLOTS"] = "replicate=4,index=0"
	if cfg, err = loadTestConfig(t, env); err != nil || len(cfg.Node.Slots) != 1 || cfg.Node.Slots["replicate"] != 4 {
		t.Fatalf("loopback http with slots: %+v, %v", cfg.Node.Slots, err)
	}
	env["MASTER_URL"] = "http://paper.example.com"
	if _, err := loadTestConfig(t, env); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("plain http to a remote master must be rejected, got %v", err)
	}
}

func TestMasterKinds(t *testing.T) {
	values := configEnv(t)
	values["WORKER_KINDS"] = "none"
	cfg, err := loadTestConfig(t, values)
	if err != nil || len(cfg.Worker.Kinds) != 0 {
		t.Fatalf("none: %v %v", cfg.Worker.Kinds, err)
	}
	values["WORKER_KINDS"] = "ingest"
	if _, err := loadTestConfig(t, values); err == nil {
		t.Fatal("ingest always stays on the master and is not a WORKER_KINDS value")
	}
}
