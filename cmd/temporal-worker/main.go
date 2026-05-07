package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	temporalAddr := temporalAddressFromEnv()

	c, err := client.Dial(client.Options{HostPort: temporalAddr})
	if err != nil {
		logger.Error("temporal_worker.client", "addr", temporalAddr, "error", err)
		os.Exit(1)
	}
	defer c.Close()

	repo, cleanupRepo, err := newProductRepositoryFromEnv(context.Background(), logger)
	if err != nil {
		logger.Error("temporal_worker.repository", "error", err)
		os.Exit(1)
	}
	defer cleanupRepo()
	wcClient := woocommerce.NewClient(woocommerce.Config{
		BaseURL:        getenv("ECOMMERCE_WC_BASE_URL", ""),
		ConsumerKey:    getenv("ECOMMERCE_WC_CONSUMER_KEY", ""),
		ConsumerSecret: getenv("ECOMMERCE_WC_CONSUMER_SECRET", ""),
	}, &http.Client{Timeout: 10 * time.Second})
	syncEngine := enginesync.NewEngine(enginesync.Config{ProductRepository: repo, WooCommerce: wcClient, DefaultCurrency: "AUD"})
	activities := ecworkflow.NewProductPublishActivities(ecworkflow.ProductPublishActivityDeps{
		Products:  repo,
		Publisher: syncPublisher{engine: syncEngine},
		Recorder:  logRecorder{logger: logger},
	})

	w := worker.New(c, ecworkflow.TaskQueue, worker.Options{})
	w.RegisterWorkflow(ecworkflow.ProductPublishWorkflow)
	w.RegisterActivityWithOptions(activities.CheckCompliance, activity.RegisterOptions{Name: ecworkflow.CheckComplianceActivity})
	w.RegisterActivityWithOptions(activities.ValidateMedia, activity.RegisterOptions{Name: ecworkflow.ValidateMediaActivity})
	w.RegisterActivityWithOptions(activities.PublishToWooCommerce, activity.RegisterOptions{Name: ecworkflow.PublishToWooCommerceActivity})
	w.RegisterActivityWithOptions(activities.RecordWorkflowEvent, activity.RegisterOptions{Name: ecworkflow.RecordWorkflowEventActivity})

	logger.Info("temporal_worker.start", "task_queue", ecworkflow.TaskQueue, "addr", temporalAddr)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("temporal_worker.run", "error", err)
		os.Exit(1)
	}
}

type syncPublisher struct {
	engine interface {
		PublishToWooCommerce(context.Context, uuid.UUID) error
	}
}

func (p syncPublisher) PublishToWooCommerce(ctx context.Context, productID string) error {
	id, err := uuid.Parse(productID)
	if err != nil {
		return fmt.Errorf("invalid product id: %w", err)
	}
	return p.engine.PublishToWooCommerce(ctx, id)
}

type logRecorder struct {
	logger *slog.Logger
}

func (r logRecorder) RecordWorkflowEvent(_ context.Context, event ecworkflow.WorkflowEvent) error {
	if r.logger != nil {
		r.logger.Info("product_publish.event", "type", event.Type, "product_id", event.ProductID, "status", event.Status)
	}
	return nil
}

func temporalAddressFromEnv() string {
	return firstNonEmpty(
		getenv("ECOMMERCE_TEMPORAL_ADDR", ""),
		getenv("ECOMMERCE_TEMPORAL_ADDRESS", ""),
		"127.0.0.1:7233",
	)
}

func newProductRepositoryFromEnv(ctx context.Context, logger *slog.Logger) (port.ProductRepository, func(), error) {
	dsn := strings.TrimSpace(os.Getenv("ECOMMERCE_DB_URL"))
	if dsn == "" {
		if logger != nil {
			logger.Warn("temporal_worker.repository_inmemory", "reason", "ECOMMERCE_DB_URL not set")
		}
		return inmemory.NewProductRepository(), func() {}, nil
	}

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(connectCtx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("create product database pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("connect product database: %w", err)
	}
	return postgres.NewProductRepository(pool), pool.Close, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
