package billing

import (
	"context"
	"testing"
)

func TestDispatcherEmptyEventID(t *testing.T) {
	t.Parallel()
	svc, _, _ := newTestService(t)
	d := NewDispatcher(svc)
	if _, err := d.Dispatch(context.Background(), []byte(`{"type":"x","data":{"object":{}}}`)); err == nil {
		t.Fatalf("expected error for missing id")
	}
}

func TestDispatcherNilService(t *testing.T) {
	t.Parallel()
	var d *Dispatcher
	if _, err := d.Dispatch(context.Background(), []byte(`{"id":"x","type":"y","data":{"object":{}}}`)); err == nil {
		t.Fatalf("expected error for nil dispatcher")
	}
	d = &Dispatcher{}
	if _, err := d.Dispatch(context.Background(), []byte(`{"id":"x"}`)); err == nil {
		t.Fatalf("expected error for nil service in dispatcher")
	}
}

func TestDispatcherSubscriptionDeletedNotFoundIsNoOp(t *testing.T) {
	t.Parallel()
	svc, _, _ := newTestService(t)
	d := NewDispatcher(svc)
	payload := `{"id":"evt_d","type":"customer.subscription.deleted","data":{"object":{"id":"sub_missing","metadata":{"tenant_id":"tenant-z"}}}}`
	id, err := d.Dispatch(context.Background(), []byte(payload))
	if err != nil {
		t.Fatalf("Dispatch on missing should be no-op, got %v", err)
	}
	if id != "evt_d" {
		t.Fatalf("id = %q", id)
	}
}

func TestDispatcherRejectsMissingTenantOnInvoice(t *testing.T) {
	t.Parallel()
	svc, _, _ := newTestService(t)
	d := NewDispatcher(svc)
	payload := `{"id":"evt_inv","type":"invoice.payment_succeeded","data":{"object":{"id":"inv_x","subscription":"","metadata":{"tenant_id":""}}}}`
	if _, err := d.Dispatch(context.Background(), []byte(payload)); err == nil {
		t.Fatalf("expected error for missing tenant on invoice")
	}
}
