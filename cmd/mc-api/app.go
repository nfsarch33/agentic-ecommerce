package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/lifecycle"
	"github.com/nfsarch33/agentic-ecommerce/internal/memwatch"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/observability/hooks"
	"github.com/nfsarch33/agentic-ecommerce/internal/runtimeobs"
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

	shutdownTimeout := parseDurationEnv("ECOMMERCE_SHUTDOWN_TIMEOUT", 10*time.Second)
	if shutdownTimeout <= 0 {
		shutdownTimeout = 10 * time.Second
	}
	mgr := lifecycle.New(logger, shutdownTimeout)
	reg, h := startObservability(mgr, logger, "mc-api")
	ecRegistry.Store(reg)
	ecHooks.Store(h)
	defer ecRegistry.Store(nil)
	defer ecHooks.Store(nil)
	srv := newServer(logger, repo, orderRepo, cartRepo)
	defer srv.Close()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return runServerWithLifecycle(ctx, mgr, logger, httpServer)
}

// runServer drives the http.Server lifecycle: start in a goroutine,
// wait for either a server error or context cancellation, then
// gracefully shut down. Pure function of (ctx, logger, server,
// timeout) so tests inject a httptest-listened *http.Server and
// cancel the context to exercise the shutdown branch.
//
// v2.10.0 Story 1: the function delegates to a lifecycle.Manager via
// runServerWithLifecycle so all binaries share one drain protocol.
func runServer(ctx context.Context, logger *slog.Logger, server *http.Server, shutdownTimeout time.Duration) int {
	if shutdownTimeout <= 0 {
		shutdownTimeout = 10 * time.Second
	}
	mgr := lifecycle.New(logger, shutdownTimeout)
	return runServerWithLifecycle(ctx, mgr, logger, server)
}

// runServerWithLifecycle starts an http.Server in the background and
// closes it via the supplied lifecycle.Manager. The caller is
// responsible for registering additional Closers + invoking
// mgr.Shutdown when appropriate.
func runServerWithLifecycle(ctx context.Context, mgr *lifecycle.Manager, logger *slog.Logger, server *http.Server) int {
	mgr.Register("http-server", lifecycle.CloserFunc(func(closeCtx context.Context) error {
		return server.Shutdown(closeCtx)
	}))

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	select {
	case err := <-errCh:
		_ = mgr.Shutdown()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("mc-api.stop", "error", err)
			return 1
		}
		return 0
	case <-ctx.Done():
		logger.Info("mc-api.shutdown")
		closeErr := mgr.Shutdown()
		// drain the listen goroutine so its error doesn't leak past return.
		listenErr := <-errCh
		if closeErr != nil {
			logger.Error("mc-api.shutdown_failed", "error", closeErr)
			return 1
		}
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			// listen-time bind errors after shutdown are still benign because
			// ctx fired first; report success to preserve the legacy contract.
			logger.Info("mc-api.listen_returned", "error", listenErr)
		}
		return 0
	}
}

// startObservability boots the v2.10.0 metric registry + memwatch
// sampler and registers them with the supplied lifecycle.Manager.
// Returns the registry plus the v6.2.1 observability hooks bundle so
// the caller can wire the workerpool / breaker / coordinator port
// adapters into any current or future composition root call site.
//
// v6.2.0 (PR #134) introduced internal/runtimeobs to wrap registry
// construction + memwatch sink + evomap runtime sample emission.
// v6.2.1 QA layers the hooks.FromRegistry bundle on top of that
// wrapper: the runtimeobs.Registry() is the same *metrics.Registry
// that hooks.FromRegistry adapts, so the workerpool / breaker /
// coord port interfaces and the evomap runtime samples write into
// the same scrape surface.
func startObservability(mgr *lifecycle.Manager, logger *slog.Logger, binary string) (*metrics.Registry, *hooks.Hooks) {
	rt := runtimeobs.New(logger, binary, runtimeobs.Config{
		EvomapPath: runtimeobs.DefaultEvomapPath(os.Getenv),
		Rotate:     true,
	})
	reg := rt.Registry()
	sampler := memwatch.NewSampler(logger, memwatch.Config{
		BinaryName:        binary,
		SampleInterval:    5 * time.Second,
		Sink:              rt,
		HeapAlarmCallback: func() { reg.OOMAlarms.Inc(metrics.Labels{}) },
	})
	go func() { _ = sampler.Run(context.Background()) }()
	mgr.Register("memwatch", sampler)
	mgr.Register("runtime-observability", rt)
	return reg, hooks.FromRegistry(reg)
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
