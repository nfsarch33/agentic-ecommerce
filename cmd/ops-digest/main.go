// main wires the digest: Woo credentials from the environment (the
// fixture env file renders WOO_BASE_URL/WOO_CONSUMER_KEY/WOO_CONSUMER_SECRET),
// output under ~/runs/ops-digest, optional Slack webhook, exit 0 only when
// the digest rendered (a store that cannot be read is a red night, not a
// silent gap in the week of digests).
package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("ops-digest failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseURL := os.Getenv("WOO_BASE_URL")
	if baseURL == "" {
		return fmt.Errorf("WOO_BASE_URL is unset")
	}
	client := woocommerce.NewClient(woocommerce.Config{
		BaseURL:        baseURL,
		ConsumerKey:    os.Getenv("WOO_CONSUMER_KEY"),
		ConsumerSecret: os.Getenv("WOO_CONSUMER_SECRET"),
	}, &http.Client{Timeout: 30 * time.Second})

	digest, err := collectDigest(ctx, client, &http.Client{Timeout: 10 * time.Second}, baseURL, time.Now())
	if err != nil {
		return err
	}
	body := renderDigest(digest)

	outDir := os.Getenv("OPS_DIGEST_OUT")
	if outDir == "" {
		home, _ := os.UserHomeDir()
		outDir = filepath.Join(home, "runs", "ops-digest")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}
	path := filepath.Join(outDir, "ops-digest-"+digest.Date+".txt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // G306 world-readable digest by design: no secrets in it
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Print(body)
	logger.Info("ops digest written", slog.String("path", path), slog.Bool("reconciled", digest.ReportAvailable && digest.Reconciled))

	if webhook := os.Getenv("OPS_DIGEST_SLACK_WEBHOOK"); webhook != "" {
		if err := postSlack(ctx, webhook, digest.Date, body); err != nil {
			// Delivery failure must not fail the digest: the file on disk is
			// the record; Slack is a convenience.
			logger.Warn("slack post failed; digest file remains the record", slog.String("error", err.Error()))
		}
	}
	return nil
}

func postSlack(ctx context.Context, webhook, date, text string) error {
	payload := fmt.Sprintf("{\"text\":\"ops digest %s\\n```%s```\"}", date, text)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader([]byte(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack: HTTP %d", resp.StatusCode)
	}
	return nil
}
