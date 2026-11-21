// Package serve 提供带超时与优雅关闭的 HTTP 启动器。
// 四个二进制共用，避免各自裸调 ListenAndServe。
package serve

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Config 是启动参数。
type Config struct {
	Name        string
	Addr        string
	Handler     http.Handler
	ReadTimeout time.Duration
	IdleTimeout time.Duration
	// DisableReadTimeout 用于长连接场景（WebSocket）。
	// http.Server.ReadTimeout 会把已 hijack 的连接也一起切断，
	// 表现为「客户端收到 connection reset」。
	DisableReadTimeout bool
}

// Run 启动服务并阻塞，收到 SIGINT/SIGTERM 后优雅关闭。
func Run(cfg Config) error {
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 15 * time.Second
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 60 * time.Second
	}
	readTimeout := cfg.ReadTimeout
	if cfg.DisableReadTimeout {
		readTimeout = 0
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           cfg.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       cfg.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("%s listening on %s", cfg.Name, cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Printf("%s shutting down", cfg.Name)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
