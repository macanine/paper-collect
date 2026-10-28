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

	conn, err := db.Open(cfg.DatabaseDriver, cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	if err := db.Migrate(conn); err != nil {
		return fmt.Errorf("迁移数据库失败: %w", err)
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
		DB:      conn,
		Blobs:   store,
		Box:     box,
		Signer:  signer,
		Limiter: limiter,
		Factory: storage.New,
		Logf:    log.Printf,
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
	if err := svc.RecoverInterrupted(ctx); err != nil {
		return fmt.Errorf("恢复中断任务失败: %w", err)
	}
	if queued, err := svc.EnqueueMissingIndexes(ctx); err != nil {
		log.Printf("补建未索引资料失败: %v", err)
	} else if queued > 0 {
		log.Printf("已为 %d 份未索引资料补建任务", queued)
	}
	svc.StartWorker(ctx)

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
