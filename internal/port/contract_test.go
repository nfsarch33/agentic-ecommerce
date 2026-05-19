package port_test

// The port package exposes the Clean Architecture interface boundaries that
// every adapter (postgres, inmemory, woocommerce, ...) must satisfy. This
// file is a contract test that fails at compile time if a concrete adapter
// stops satisfying the published interface, and at runtime if the port
// value-types diverge from their documented invariants.
//
// Coverage rationale: the port package itself contains no executable
// statements (interface declarations only), so this file contributes 0% to
// the package-level coverage figure. Its real value is preventing silent
// breakage of the public boundary used by every other package.

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/adapter/inmemory"
	"github.com/nfsarch33/helixon-ec/internal/adapter/notification"
	"github.com/nfsarch33/helixon-ec/internal/adapter/objectstore"
	"github.com/nfsarch33/helixon-ec/internal/adapter/postgres"
	"github.com/nfsarch33/helixon-ec/internal/adapter/signedurl"
	stripeadapter "github.com/nfsarch33/helixon-ec/internal/adapter/stripe"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/domain/digital"
	"github.com/nfsarch33/helixon-ec/internal/domain/membership"
	orderdomain "github.com/nfsarch33/helixon-ec/internal/domain/order"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

// Compile-time interface satisfaction checks.
//
// These compile-only assertions form the authoritative contract: deleting or
// renaming any method on the listed adapters here is a compile error, which
// keeps the public boundary stable across releases.

var (
	_ port.ProductRepository       = (*inmemory.ProductRepository)(nil)
	_ port.ProductRepository       = (*postgres.ProductRepository)(nil)
	_ port.TenantProductRepository = (*postgres.ProductRepository)(nil)

	_ port.OrderRepository       = (*inmemory.OrderRepository)(nil)
	_ port.OrderRepository       = (*postgres.OrderRepository)(nil)
	_ port.TenantOrderRepository = (*postgres.OrderRepository)(nil)

	_ port.CartRepository = (*inmemory.CartRepository)(nil)
	_ port.CartRepository = (*postgres.CartRepository)(nil)

	_ port.MediaStore = (*objectstore.LocalStore)(nil)
	_ port.MediaStore = (*objectstore.CloudStub)(nil)

	_ port.MembershipRepository         = (*inmemory.MembershipRepository)(nil)
	_ port.MembershipRepository         = (*postgres.MembershipRepository)(nil)
	_ port.MembershipPaymentGateway     = (*stripeadapter.PaymentGateway)(nil)
	_ port.MembershipNotificationSender = (*notification.MembershipNotificationRecorder)(nil)

	// v2.3.0 Digital goods bounded context.
	_ port.DigitalProductRepository = (*inmemory.DigitalProductRepository)(nil)
	_ port.DigitalProductRepository = (*postgres.DigitalProductRepository)(nil)
	_ port.LicenseRepository        = (*inmemory.LicenseRepository)(nil)
	_ port.LicenseRepository        = (*postgres.LicenseRepository)(nil)
	_ port.AccessGrantRepository    = (*inmemory.AccessGrantRepository)(nil)
	_ port.AccessGrantRepository    = (*postgres.AccessGrantRepository)(nil)
	_ port.DownloadTokenIssuer      = (*signedurl.HMACIssuer)(nil)
)

func TestPortListResultZeroValueIsEmpty(t *testing.T) {
	t.Parallel()

	var got port.ListResult
	if got.Total != 0 {
		t.Fatalf("zero ListResult.Total = %d, want 0", got.Total)
	}
	if got.Products != nil {
		t.Fatalf("zero ListResult.Products = %v, want nil", got.Products)
	}
}

func TestPortPaymentAuthorizationCarriesAmount(t *testing.T) {
	t.Parallel()

	amount, err := catalog.NewMoney(1995, "AUD")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	auth := port.PaymentAuthorization{ID: "auth-1", Amount: amount}
	if auth.ID == "" {
		t.Fatal("PaymentAuthorization.ID should not be empty")
	}
	if auth.Amount.Amount() != 1995 || auth.Amount.Currency() != "AUD" {
		t.Fatalf("amount = %d %s, want 1995 AUD", auth.Amount.Amount(), auth.Amount.Currency())
	}
}

func TestPortShipmentRoundTrip(t *testing.T) {
	t.Parallel()

	shipment := port.Shipment{ID: "shp-1", Carrier: "auspost", Status: "label_created"}
	if shipment.Carrier == "" || shipment.Status == "" {
		t.Fatalf("Shipment fields should round-trip: %+v", shipment)
	}
}

func TestPortMediaObjectRoundTrip(t *testing.T) {
	t.Parallel()

	body := strings.NewReader("hello world")
	obj := port.MediaObject{Key: "uploads/test.txt", ContentType: "text/plain", Body: body}

	if obj.Body == nil {
		t.Fatal("MediaObject.Body should not be nil")
	}
	if obj.ContentType != "text/plain" {
		t.Fatalf("ContentType = %q, want text/plain", obj.ContentType)
	}
	out, err := io.ReadAll(obj.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(out) != "hello world" {
		t.Fatalf("body = %q, want hello world", string(out))
	}
}

func TestPortStoredMediaObjectInvariants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	stored := port.StoredMediaObject{
		Key:         "uploads/test.png",
		URL:         "https://media.example/test.png",
		ContentType: "image/png",
		SizeBytes:   1024,
		StoredAt:    now,
	}

	if stored.Key == "" {
		t.Fatal("StoredMediaObject.Key required")
	}
	if stored.URL == "" {
		t.Fatal("StoredMediaObject.URL required")
	}
	if stored.SizeBytes < 0 {
		t.Fatalf("SizeBytes = %d, must be non-negative", stored.SizeBytes)
	}
	if !stored.StoredAt.Equal(now) {
		t.Fatalf("StoredAt = %s, want %s", stored.StoredAt, now)
	}
}

// TestPortAICompletionRequestSupportsOptionalKnobs documents that the
// optional pointer fields preserve nil semantics so adapters can rely on
// "unset" being distinguishable from explicit zero values.
func TestPortAICompletionRequestSupportsOptionalKnobs(t *testing.T) {
	t.Parallel()

	temperature := 0.4
	maxTokens := 256
	req := port.AICompletionRequest{
		Model:       "embo-01",
		Messages:    []port.AIMessage{{Role: "user", Content: "ping"}},
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	}

	if req.Temperature == nil || *req.Temperature != 0.4 {
		t.Fatalf("Temperature = %v, want 0.4", req.Temperature)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 256 {
		t.Fatalf("MaxTokens = %v, want 256", req.MaxTokens)
	}

	zero := port.AICompletionRequest{Model: "embo-01"}
	if zero.Temperature != nil || zero.MaxTokens != nil {
		t.Fatalf("zero request should leave optional knobs nil: %+v", zero)
	}
}

// TestPortFakeRepositoryStaysAssignableToProductRepository keeps a
// minimal in-test fake honest. If the port shape changes (e.g. a new
// method is added), this test fails to compile so we catch the contract
// drift before downstream packages.
func TestPortFakeRepositoryStaysAssignableToProductRepository(t *testing.T) {
	t.Parallel()

	var _ port.ProductRepository = (*portContractFakeProductRepo)(nil)
	var _ port.OrderRepository = (*portContractFakeOrderRepo)(nil)
	var _ port.CartRepository = (*portContractFakeCartRepo)(nil)
	var _ port.PaymentGateway = (*portContractFakePaymentGateway)(nil)
	var _ port.ShippingProvider = (*portContractFakeShippingProvider)(nil)
	var _ port.AITextGenerator = (*portContractFakeAITextGenerator)(nil)
	var _ port.ProductChannel = (*portContractFakeProductChannel)(nil)
	var _ port.MembershipRepository = (*portContractFakeMembershipRepo)(nil)
	var _ port.MembershipPaymentGateway = (*portContractFakeMembershipPaymentGateway)(nil)
	var _ port.MembershipNotificationSender = (*portContractFakeMembershipNotifier)(nil)
	var _ port.DigitalProductRepository = (*portContractFakeDigitalProductRepo)(nil)
	var _ port.LicenseRepository = (*portContractFakeLicenseRepo)(nil)
	var _ port.AccessGrantRepository = (*portContractFakeAccessGrantRepo)(nil)
	var _ port.DownloadTokenIssuer = (*portContractFakeDownloadIssuer)(nil)
}

type portContractFakeDigitalProductRepo struct{}

func (portContractFakeDigitalProductRepo) Create(context.Context, string, digital.DigitalProduct) error {
	return nil
}
func (portContractFakeDigitalProductRepo) Update(context.Context, string, digital.DigitalProduct) error {
	return nil
}
func (portContractFakeDigitalProductRepo) Get(context.Context, string, uuid.UUID) (digital.DigitalProduct, error) {
	return digital.DigitalProduct{}, nil
}
func (portContractFakeDigitalProductRepo) List(context.Context, string, int, int) (port.DigitalProductList, error) {
	return port.DigitalProductList{}, nil
}
func (portContractFakeDigitalProductRepo) Delete(context.Context, string, uuid.UUID) error {
	return nil
}

type portContractFakeLicenseRepo struct{}

func (portContractFakeLicenseRepo) Create(context.Context, string, digital.License) error {
	return nil
}
func (portContractFakeLicenseRepo) Get(context.Context, string, uuid.UUID) (digital.License, error) {
	return digital.License{}, nil
}
func (portContractFakeLicenseRepo) List(context.Context, string, int, int) (port.LicenseList, error) {
	return port.LicenseList{}, nil
}
func (portContractFakeLicenseRepo) ListByCustomer(context.Context, string, uuid.UUID, int, int) (port.LicenseList, error) {
	return port.LicenseList{}, nil
}
func (portContractFakeLicenseRepo) SaveState(context.Context, string, digital.License) error {
	return nil
}

type portContractFakeAccessGrantRepo struct{}

func (portContractFakeAccessGrantRepo) Upsert(context.Context, string, digital.AccessGrant) error {
	return nil
}
func (portContractFakeAccessGrantRepo) Get(context.Context, string, uuid.UUID) (digital.AccessGrant, error) {
	return digital.AccessGrant{}, nil
}
func (portContractFakeAccessGrantRepo) ListByCustomer(context.Context, string, uuid.UUID, int, int) (port.AccessGrantList, error) {
	return port.AccessGrantList{}, nil
}
func (portContractFakeAccessGrantRepo) GetByCustomerProduct(context.Context, string, uuid.UUID, uuid.UUID) (digital.AccessGrant, error) {
	return digital.AccessGrant{}, nil
}

type portContractFakeDownloadIssuer struct{}

func (portContractFakeDownloadIssuer) Issue(port.IssueDownloadRequest) (port.IssueDownloadResponse, error) {
	return port.IssueDownloadResponse{}, nil
}
func (portContractFakeDownloadIssuer) Verify(string, time.Time) (port.DownloadClaims, error) {
	return port.DownloadClaims{}, nil
}

// TestPortMembershipNotificationEventCarriesTenant ensures the value type
// stays useful for downstream adapters even at zero value.
func TestPortMembershipNotificationEventCarriesTenant(t *testing.T) {
	t.Parallel()
	evt := port.MembershipNotificationEvent{
		TenantID:   "tenant-a",
		State:      membership.StateActive,
		Transition: membership.TransitionActivate,
		OccurredAt: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}
	if evt.TenantID != "tenant-a" {
		t.Fatalf("tenant = %q, want tenant-a", evt.TenantID)
	}
	if evt.State != membership.StateActive {
		t.Fatalf("state = %q, want active", evt.State)
	}
}

type portContractFakeMembershipRepo struct{}

func (portContractFakeMembershipRepo) CreatePlan(context.Context, string, membership.MembershipPlan) error {
	return nil
}

func (portContractFakeMembershipRepo) UpdatePlan(context.Context, string, membership.MembershipPlan) error {
	return nil
}

func (portContractFakeMembershipRepo) GetPlan(context.Context, string, uuid.UUID) (membership.MembershipPlan, error) {
	return membership.MembershipPlan{}, nil
}

func (portContractFakeMembershipRepo) ListPlans(context.Context, string, int, int) (port.MembershipPlanList, error) {
	return port.MembershipPlanList{}, nil
}

func (portContractFakeMembershipRepo) DeletePlan(context.Context, string, uuid.UUID) error {
	return nil
}

func (portContractFakeMembershipRepo) CreateMember(context.Context, string, membership.Member) error {
	return nil
}

func (portContractFakeMembershipRepo) GetMember(context.Context, string, uuid.UUID) (membership.Member, error) {
	return membership.Member{}, nil
}

func (portContractFakeMembershipRepo) ListMembers(context.Context, string, int, int) (port.MembershipMemberList, error) {
	return port.MembershipMemberList{}, nil
}

func (portContractFakeMembershipRepo) CreateSubscription(context.Context, string, membership.Subscription) error {
	return nil
}

func (portContractFakeMembershipRepo) SaveSubscription(context.Context, string, membership.Subscription) error {
	return nil
}

func (portContractFakeMembershipRepo) GetSubscription(context.Context, string, uuid.UUID) (membership.Subscription, error) {
	return membership.Subscription{}, nil
}

func (portContractFakeMembershipRepo) GetSubscriptionByMember(context.Context, string, uuid.UUID) (membership.Subscription, error) {
	return membership.Subscription{}, nil
}

func (portContractFakeMembershipRepo) ListSubscriptions(context.Context, string, int, int) (port.MembershipSubscriptionList, error) {
	return port.MembershipSubscriptionList{}, nil
}

type portContractFakeMembershipPaymentGateway struct{}

func (portContractFakeMembershipPaymentGateway) CreateSubscription(context.Context, port.CreateSubscriptionRequest) (port.CreateSubscriptionResponse, error) {
	return port.CreateSubscriptionResponse{}, nil
}

func (portContractFakeMembershipPaymentGateway) CancelSubscription(context.Context, port.CancelSubscriptionRequest) error {
	return nil
}

func (portContractFakeMembershipPaymentGateway) GetSubscription(context.Context, port.GetSubscriptionRequest) (port.PaymentSubscriptionStatus, error) {
	return port.PaymentSubscriptionStatus{}, nil
}

type portContractFakeMembershipNotifier struct{}

func (portContractFakeMembershipNotifier) SendMembershipEvent(context.Context, port.MembershipNotificationEvent) error {
	return nil
}

type portContractFakeProductRepo struct{}

func (portContractFakeProductRepo) Create(context.Context, catalog.Product) error {
	return nil
}

func (portContractFakeProductRepo) GetByID(context.Context, uuid.UUID) (catalog.Product, error) {
	return catalog.Product{}, nil
}

func (portContractFakeProductRepo) GetBySlug(context.Context, string) (catalog.Product, error) {
	return catalog.Product{}, nil
}

func (portContractFakeProductRepo) List(context.Context, int, int) (port.ListResult, error) {
	return port.ListResult{}, nil
}

func (portContractFakeProductRepo) Update(context.Context, catalog.Product) error {
	return nil
}

func (portContractFakeProductRepo) Delete(context.Context, uuid.UUID) error {
	return nil
}

type portContractFakeOrderRepo struct{}

func (portContractFakeOrderRepo) Create(context.Context, orderdomain.Order) error {
	return nil
}

func (portContractFakeOrderRepo) GetByID(context.Context, uuid.UUID) (orderdomain.Order, error) {
	return orderdomain.Order{}, nil
}

func (portContractFakeOrderRepo) UpdateStatus(context.Context, uuid.UUID, orderdomain.Status) (orderdomain.Order, error) {
	return orderdomain.Order{}, nil
}

type portContractFakeCartRepo struct{}

func (portContractFakeCartRepo) Save(context.Context, orderdomain.Cart) error {
	return nil
}

func (portContractFakeCartRepo) GetBySessionID(context.Context, string) (orderdomain.Cart, error) {
	return orderdomain.Cart{}, nil
}

type portContractFakePaymentGateway struct{}

func (portContractFakePaymentGateway) Authorize(context.Context, orderdomain.Order) (port.PaymentAuthorization, error) {
	return port.PaymentAuthorization{}, nil
}

type portContractFakeShippingProvider struct{}

func (portContractFakeShippingProvider) CreateShipment(context.Context, orderdomain.Order) (port.Shipment, error) {
	return port.Shipment{}, nil
}

type portContractFakeAITextGenerator struct{}

func (portContractFakeAITextGenerator) Complete(context.Context, port.AICompletionRequest) (port.AICompletionResponse, error) {
	return port.AICompletionResponse{}, nil
}

type portContractFakeProductChannel struct{}

func (portContractFakeProductChannel) UpsertProduct(context.Context, catalog.Product) error {
	return nil
}
