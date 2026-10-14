package graceful

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/zanehu-ai/x-go/logger"
)

// Option configures the graceful server.
type Option func(*serverConfig)

type serverConfig struct {
	shutdownTimeout time.Duration
}

// WithShutdownTimeout sets the maximum time to wait for in-flight requests during shutdown.
// Defaults to 10 seconds.
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *serverConfig) { c.shutdownTimeout = d }
}

// ListenAndServe starts an HTTP server and blocks until SIGINT/SIGTERM is received,
// then gracefully shuts down within the configured timeout.
func ListenAndServe(addr string, handler http.Handler, opts ...Option) error {
	cfg := &serverConfig{shutdownTimeout: 10 * time.Second}
	for _, opt := range opts {
		opt(cfg)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("listen: %w", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		logger.Info("shutting down", zap.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	logger.Info("server stopped")
	return nil
}
