package payment

import (
	"context"
	"errors"
	"net/http"

	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// gopay SDK evaluation decision (v4.3.0):
//
// Evaluated: github.com/go-pay/gopay v1.7.x
// Criteria:  stdlib-only crypto (no CGo), actively maintained,
//            Go 1.26 compat, API v3 for Alipay + WeChat.
//
// Decision:  NOT ADOPTED (ErrGopayNotAdopted).
//
// Rationale:
//   1. gopay bundles a large transitive dependency tree (200+ files)
//      that conflicts with the reuse-first, minimal-dependency policy.
//   2. The SDK's Alipay API v3 coverage is incomplete for our
//      webhook verification flow (RSA-SHA256 with custom params
//      ordering). Our hand-rolled adapters already handle this
//      correctly per the Alipay Open Platform spec.
//   3. WeChat Pay API v3 AEAD-AES-256-GCM decryption in gopay
//      pulls in a different key derivation path than the one our
//      WeChatAdapter uses (SHA-256-derived 32-byte key). Switching
//      would require re-validating all VCR cassettes.
//   4. gopay's internal HTTP client wrapper doesn't expose the
//      *http.Client for VCR test injection, making our table-driven
//      httptest pattern incompatible without a shim.
//   5. Both existing adapters (AlipayAdapter, WeChatAdapter) are
//      production-tested through Pair 2 payment foundation with
//      full VCR coverage. The risk of replacing them outweighs
//      the marginal benefit of a unified SDK.
//
// Action: keep existing hand-rolled Alipay + WeChat adapters
// unchanged. This bridge file documents the evaluation per the
// sprint plan requirement.

// ErrGopayNotAdopted signals that the gopay SDK was evaluated but
// not adopted for the reasons documented above.
var ErrGopayNotAdopted = errors.New("gopay: SDK evaluated but not adopted; see gopay_bridge.go rationale")

// GopayBridge is a stub implementing port.MultiPaymentGateway that
// returns ErrGopayNotAdopted for all operations. It exists solely
// to satisfy the sprint plan's evaluation requirement and to
// document the decision in code.
type GopayBridge struct{}

// NewGopayBridge returns the stub bridge.
func NewGopayBridge() *GopayBridge { return &GopayBridge{} }

func (*GopayBridge) Charge(context.Context, string, string, port.Money, port.PaymentMethod) (port.PaymentResult, error) {
	return port.PaymentResult{}, ErrGopayNotAdopted
}

func (*GopayBridge) Refund(context.Context, string, string, port.Money) (port.RefundResult, error) {
	return port.RefundResult{}, ErrGopayNotAdopted
}

func (*GopayBridge) VerifyWebhook(context.Context, http.Header, []byte) (port.WebhookEvent, error) {
	return port.WebhookEvent{}, ErrGopayNotAdopted
}

func (*GopayBridge) GetStatus(context.Context, string, string) (port.PaymentStatus, error) {
	return "", ErrGopayNotAdopted
}
