// Command store-proxy is the counting egress proxy in front of the store
// REST API (v18900-5). It holds the store REST key and injects it on
// forward; callers run credential-less and a direct call gets 401 from the
// store. Every request is audited (path only, never the query); writes
// without an approval id are refused. Fail-closed by construction.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/storeproxy"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg := storeproxy.Config{
		StoreBaseURL:   os.Getenv("ECOMMERCE_STORE_BASE_URL"),
		ConsumerKey:    os.Getenv("ECOMMERCE_STORE_CONSUMER_KEY"),
		ConsumerSecret: os.Getenv("ECOMMERCE_STORE_CONSUMER_SECRET"),
		AuditLogPath:   os.Getenv("ECOMMERCE_PROXY_AUDIT_LOG"),
	}
	addr := getenv("ECOMMERCE_PROXY_ADDR", "127.0.0.1:8090")

	proxy, err := storeproxy.NewProxy(cfg)
	if err != nil {
		log.Error("store-proxy refuses to start (fail closed)", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{Addr: addr, Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Info("store-proxy listening", "addr", addr, "store", cfg.StoreBaseURL, "audit_log", cfg.AuditLogPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("store-proxy failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = proxy.Close()
}
