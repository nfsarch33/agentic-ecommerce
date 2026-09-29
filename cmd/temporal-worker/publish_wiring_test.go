package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/publishgate"
)

// recordingStore pins WHICH workflow id the gate looks an approval up
// under: it approves exactly one id and records what it was asked for.
type recordingStore struct {
	publishgate.Store
	approvedWorkflowID string
	askedWorkflowID    string
	claimed            int
	completed          string
}

func (r *recordingStore) ApprovalFor(_ context.Context, workflowID string) (publishgate.Decision, error) {
	r.askedWorkflowID = workflowID
	if workflowID == r.approvedWorkflowID {
		return publishgate.Decision{WorkflowID: workflowID, Approved: true, Actor: "reviewer"}, nil
	}
	return publishgate.Decision{}, publishgate.ErrNoApproval
}
func (r *recordingStore) Claim(context.Context, string, string, string, string, string, string, string, time.Duration) error {
	r.claimed++
	return nil
}
func (r *recordingStore) Complete(_ context.Context, _, status, writeKind, _ string) error {
	r.completed = status + "/" + writeKind
	return nil
}
func (r *recordingStore) Fail(context.Context, string) error { return nil }

// fakeGateRemote answers lookups with not-found and counts writes.
type fakeGateRemote struct {
	creates int
	updates int
}

func (f *fakeGateRemote) FindBySKU(context.Context, string) (*publishgate.RemoteProduct, error) {
	return nil, nil
}
func (f *fakeGateRemote) Update(ctx context.Context, remoteID string, fields map[string]string) (*publishgate.RemoteProduct, error) {
	f.updates++
	return &publishgate.RemoteProduct{ID: remoteID, Fields: fields}, nil
}
func (f *fakeGateRemote) Create(ctx context.Context, fields map[string]string) (*publishgate.RemoteProduct, error) {
	f.creates++
	return &publishgate.RemoteProduct{ID: "wp-1", Fields: fields}, nil
}

// TestGatePublisherUsesThreadedWorkflowID pins review blocker 3: approvals
// are keyed on the REAL execution id (the -auto suffix from sync_handlers
// and the fingerprint ids), so the gate publisher must use the workflow id
// threaded through the activity input — never "product-publish-"+productID.
// Mutant: reconstructing the id inside PublishToWooCommerce fails both
// halves (the approval lookup misses and the publish fails closed).
func TestGatePublisherUsesThreadedWorkflowID(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	loader := &fakeProductLoader{id: pid}

	for _, exec := range []string{
		"product-publish-" + pid.String() + "-auto",
		"product-publish-" + pid.String() + "-ab12cd34ef56",
	} {
		store := &recordingStore{approvedWorkflowID: exec}
		remote := &fakeGateRemote{}
		p := gatePublisher{gate: &publishgate.Gate{Store: store, Remote: remote, Owner: "t"}, products: loader}
		if err := p.PublishToWooCommerce(context.Background(), pid.String(), exec); err != nil {
			t.Fatalf("publish under execution id %s: %v", exec, err)
		}
		if store.askedWorkflowID != exec {
			t.Fatalf("approval lookup id = %q, want the threaded execution id %q", store.askedWorkflowID, exec)
		}
		if remote.creates != 1 {
			t.Fatalf("exec %s: creates = %d, want 1", exec, remote.creates)
		}
		if store.completed != "completed/create" {
			t.Fatalf("exec %s: completed = %q, want completed/create", exec, store.completed)
		}
	}
}

// TestWorkerDepsWireApprovalStore pins review blocker 2 (worker half):
// buildWorkerDeps must pass the PGStore into
// ProductPublishActivities.Approvals — nil there means every Update-path
// decision errors "approval recorder not configured" and the workflow
// never reaches publish. Mutant: dropping Approvals from the deps literal
// fails this test. Needs a reachable ECOMMERCE_DB_URL (skips otherwise,
// matching the pgStore pattern).
func TestWorkerDepsWireApprovalStore(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ECOMMERCE_DB_URL"))
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/ecommerce?sslmode=disable"
	}
	t.Setenv("ECOMMERCE_DB_URL", dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("pg dsn unusable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("pg not reachable: %v", err)
	}
	pool.Close()

	deps, err := buildWorkerDeps(context.Background(), slog.New(slog.DiscardHandler), agentScheduleConfig{})
	if err != nil {
		t.Fatalf("buildWorkerDeps: %v", err)
	}
	if deps.PublishActivities.Approvals == nil {
		t.Fatal("PublishActivities.Approvals is nil: RecordApproval is dead and the gate will fail closed (blocker 2)")
	}
}
