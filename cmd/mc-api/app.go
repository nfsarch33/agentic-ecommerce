package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// app.go (v2.6.1 cmd/* DI refactor): keep main.go's main() body
// minimal by extracting the testable shell into mainImpl + runServer
// + getenvFn. Production wires os.Exit(mainImpl(...)); tests drive
// mainImpl directly with stub args/getenv/writer to exercise every
// branch (healthcheck OK, healthcheck fail, server-stop, graceful
// shutdown).
//
// See go-clean-architecture skill: cmd/* main reduces to dependency
// build + delegate, with errors translated to exit codes by a single
// pure function.

// mainImpl is the testable entry point. args + getenv + writer in,
// process exit code out.
func mainImpl(ctx context.Context, args []string, stdout io.Writer, getenv func(string) string) int {
	logger := slog.New(slog.NewJSONHandler(stdout, nil))
	if isHealthcheckArgs(args) {
		addr := getenvFn(getenv, "ECOMMERCE_HTTP_ADDR", "127.0.0.1:8080")
		if err := runHealthcheck(addr); err != nil {
			logger.Error("mc-api.healthcheck_failed", "error", err)
			return 1
		}
		return 0
	}

	repo := newSeededProductRepository()
	orderRepo, cartRepo := newOrderAndCartRepos()

	addr := getenvFn(getenv, "ECOMMERCE_HTTP_ADDR", "127.0.0.1:8080")
	logger.Info("mc-api.start", "addr", addr)

	srv := newServer(logger, repo, orderRepo, cartRepo)
	defer srv.Close()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return runServer(ctx, logger, httpServer, srv.cfg.shutdownTimeout)
}

// runServer drives the http.Server lifecycle: start in a goroutine,
// wait for either a server error or context cancellation, then
// gracefully shut down. Pure function of (ctx, logger, server,
// timeout) so tests inject a httptest-listened *http.Server and
// cancel the context to exercise the shutdown branch.
func runServer(ctx context.Context, logger *slog.Logger, server *http.Server, shutdownTimeout time.Duration) int {
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("mc-api.stop", "error", err)
			return 1
		}
		return 0
	case <-ctx.Done():
		logger.Info("mc-api.shutdown")
		if shutdownTimeout <= 0 {
			shutdownTimeout = 10 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("mc-api.shutdown_failed", "error", err)
			return 1
		}
		return 0
	}
}

// getenvFn is the io-injectable variant of the package-private getenv
// helper. Tests inject a closure; production wires os.Getenv. Both
// return the fallback when the configured value is empty.
func getenvFn(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}
