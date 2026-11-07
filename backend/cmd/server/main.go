// Command server runs the paper-collect HTTP/MCP API and the background worker.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/config"
	"github.com/nic/paper-collect/backend/internal/cryptobox"
	"github.com/nic/paper-collect/backend/internal/db"
	"github.com/nic/paper-collect/backend/internal/server"
	"github.com/nic/paper-collect/backend/internal/service"
	"github.com/nic/paper-collect/backend/internal/storage"
	"github.com/nic/paper-collect/backend/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cfg.Role == config.RoleWorker {
		return runWorker(cfg)
	}

	conn, err := db.Open(cfg.DatabaseDriver, cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	if cfg.AutoMigrate {
		if err := db.Migrate(conn); err != nil {
			return fmt.Errorf("迁移数据库失败: %w", err)
		}
	}

	store, err := blob.NewStore(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("初始化本地存储失败: %w", err)
	}
	box, err := cryptobox.New(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("初始化密钥盒失败: %w", err)
	}
	signer, err := auth.NewSigner(cfg.JWTSecret, time.Now)
	if err != nil {
		return fmt.Errorf("初始化 JWT 签名器失败: %w", err)
	}
	limiter := auth.NewLimiter(time.Now, 5, 15*time.Minute, 15*time.Minute)

	svc, err := service.New(service.Options{
		DB:           conn,
		Blobs:        store,
		Box:          box,
		Signer:       signer,
		Limiter:      limiter,
		Factory:      storage.New,
		Logf:         log.Printf,
		PullCacheTTL: cfg.PullCacheTTL,
		Worker: service.WorkerSettings{
			Kinds:                cfg.Worker.Kinds,
			IngestConcurrency:    cfg.Worker.IngestConcurrency,
			ReplicateConcurrency: cfg.Worker.ReplicateConcurrency,
			AnalyzeConcurrency:   cfg.Worker.AnalyzeConcurrency,
			IndexConcurrency:     cfg.Worker.IndexConcurrency,
			LeaseDuration:        cfg.Worker.LeaseDuration,
			HeartbeatInterval:    cfg.Worker.HeartbeatInterval,
			PollInterval:         cfg.Worker.PollInterval,
		},
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := svc.Bootstrap(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		if errors.Is(err, service.ErrBootstrapNeeded) {
			return err
		}
		return fmt.Errorf("初始化管理员失败: %w", err)
	}
	if _, err := svc.RegisterMaster(ctx, cfg.NodeName, version()); err != nil {
		return fmt.Errorf("登记主端节点失败: %w", err)
	}
	if err := svc.RecoverInterrupted(ctx); err != nil {
		return fmt.Errorf("恢复中断任务失败: %w", err)
	}
	// Formula postings are an additive upgrade of already indexed text. Build
	// them in bounded background transactions so a large archive does not
	// delay startup; a formula search also completes any remaining backfill.
	go func() {
		indexed, err := svc.BackfillMathIndex(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("补建公式索引失败: %v", err)
		} else if err == nil && indexed > 0 {
			log.Printf("已为 %d 道已有题目补建公式索引", indexed)
		}
	}()
	if queued, err := svc.EnqueueMissingIndexes(ctx); err != nil {
		log.Printf("补建未索引资料失败: %v", err)
	} else if queued > 0 {
		log.Printf("已为 %d 份未索引资料补建任务", queued)
	}
	svc.StartWorker(ctx)
	svc.StartMasterHeartbeat(ctx)

	srv, err := server.New(server.Options{Service: svc, WebDist: cfg.WebDist})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("关闭 HTTP 服务失败: %v", err)
		}
	}()

	log.Printf("paper-collect 监听 %s（driver=%s）", cfg.Addr, cfg.DatabaseDriver)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// runWorker runs a database-free node that executes jobs leased by the master.
func runWorker(cfg config.Config) error {
	w, err := worker.New(worker.Config{
		MasterURL: cfg.Node.MasterURL, Token: cfg.Node.Token, Version: version(),
		CacheDir: cfg.Node.CacheDir, CacheMax: cfg.Node.CacheMax, Slots: cfg.Node.Slots, RapidOCRURL: cfg.Node.RapidOCRURL,
		Factory: storage.New, Logf: log.Printf,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("paper-collect 从端已连接 %s（任务槽位 %v）", cfg.Node.MasterURL, cfg.Node.Slots)
	w.Run(ctx)
	return nil
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 12 {
				return setting.Value[:12]
			}
		}
		if info.Main.Version != "" {
			return info.Main.Version
		}
	}
	return "dev"
}
