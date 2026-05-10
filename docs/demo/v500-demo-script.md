# v5.0.0 Demo Script

30-minute live demonstration of the Agentic Ecommerce platform, expanding on the v4.0.0 demo with all capabilities shipped in the v4.1.0 through v4.19.1 cycle.

**Prerequisites**: Docker Compose stack running (`make dev` or full-stack compose), a provisioned test tenant, and the frontend dev server at `http://localhost:3000`.

---

## 1. Onboarding Wizard (2 min)

**Goal**: Show the AI-guided tenant setup experience.

1. Navigate to `/register` on the frontend
2. Complete the 4-step registration wizard:
   - Step 1: Business details (name, category, region)
   - Step 2: Channel selection (TikTok, Facebook, Instagram, WooCommerce)
   - Step 3: Channel pre-flight checks (API credential validation)
   - Step 4: Plan selection (Free / Starter / Pro)
3. Show the welcome dashboard with channel status indicators
4. **Key point**: The wizard validates channel readiness before activation, preventing broken integrations

---

## 2. Payment Flow (3 min)

**Goal**: Demonstrate the 4-provider payment gateway with Stripe end-to-end.

1. Create a test product and add to cart
2. Proceed to checkout, select Stripe as payment method
3. Complete payment with test card (`4242 4242 4242 4242`)
4. Show the Stripe webhook arriving at `/webhooks/stripe`
5. Observe order status transition: `pending` -> `payment_confirmed` -> `processing`
6. Open the payment dashboard (`/payments`) showing transaction history
7. **Key point**: Webhook signature verification (HMAC-SHA256) + idempotent processing + 5-min replay window

---

## 3. Multi-Channel Listing (3 min)

**Goal**: Show simultaneous product listing across 4+ channels.

1. Select an enriched product from the catalog
2. Open the channel router and select: TikTok Shop + Facebook + Instagram + Pinterest
3. Trigger simultaneous listing creation
4. Show the channel status dashboard with per-channel sync status
5. Demonstrate inventory sync: reduce stock on one channel, observe propagation
6. **Key point**: Unified listing format adapted per platform; inventory sync prevents overselling

---

## 4. Pricing Agent (3 min)

**Goal**: Demonstrate AI-driven dynamic pricing with competitor awareness.

1. Navigate to the pricing dashboard
2. Show a competitor price undercut detected by the scraper
3. Observe the dynamic pricing agent propose a price adjustment
4. Show the margin guardrails preventing below-floor pricing
5. Approve the adjustment and see it propagate to listed channels
6. **Key point**: Guardrails enforce minimum margin; operator approval required for adjustments exceeding threshold

---

## 5. Customer Inquiry Handling (3 min)

**Goal**: Show bilingual customer service automation.

1. Simulate an inbound customer message (Chinese language) via the CS adapter
2. Show the bilingual enquiry classifier categorising the message
3. Observe the FAQ auto-responder generating a reply
4. Show the operator alert when the query requires human escalation
5. Demonstrate the multi-channel messaging adapter (TikTok + Facebook response)
6. **Key point**: Classifier handles 7 languages; FAQ responds within 30s; escalation triggers operator alert

---

## 6. Margin Dashboard (3 min)

**Goal**: Show the analytics surface for business intelligence.

1. Navigate to `/margin-dashboard`
2. Show the ROI heatmap with top 20 SKUs by margin
3. Apply the dead-stock filter to identify underperforming products
4. Drill into commission breakdown per channel (TikTok vs Facebook vs WooCommerce)
5. Switch to the daily margin rollup chart with date range picker
6. **Key point**: Real-time margin visibility across all channels with commission-aware P&L

---

## 7. MADRL Coordination (2 min)

**Goal**: Demonstrate multi-agent conflict resolution.

1. Show the MADRL coordination log
2. Create a scenario: pricing agent wants to lower price, fulfilment agent flags stock risk
3. Observe the weighted resolution: pricing gets 60% weight, fulfilment 40%
4. Show the resolution outcome with audit trail
5. **Key point**: Weighted conflict resolution prevents agent deadlocks; all decisions are auditable

---

## 8. OOM Prevention (2 min)

**Goal**: Demonstrate the backpressure and recovery mechanism.

1. Show the adaptive worker pool dashboard (current pool size, RSS usage)
2. Trigger a load spike (concurrent API requests)
3. Observe backpressure: pool shrinks, circuit breakers open, 503 responses during pressure
4. Show the phased drain in the monitoring dashboard
5. Wait for recovery: circuit breakers close, pool resizes, normal 200 responses resume
6. **Key point**: System degrades gracefully under pressure and self-recovers without operator intervention

---

## 9. Cloud Deployment (3 min)

**Goal**: Show the production deployment path.

1. Show the Helm chart structure (`deploy/helm/agentic-ecommerce/`)
2. Walk through `values-gke.yaml` (resource limits, KEDA autoscaling rules, health probes)
3. Demonstrate `helm template` output for the GKE target
4. Show Terraform modules: GKE cluster + node pools + networking
5. Show the health probe endpoints: `/healthz` (liveness), `/readyz` (dependency check)
6. Show KEDA HPA: CPU + queue-depth based autoscaling rules
7. **Key point**: Single `helm upgrade` deploys all services; KEDA auto-scales based on workload

---

## 10. Agentrace (2 min)

**Goal**: Show the agent observability and evolution tracking surface.

1. Navigate to the Grafana Agentrace dashboard
2. Show session insights: agent decision traces with latency breakdown
3. Show the EvoMap KPI rollup: daily aggregate of agent performance metrics
4. Demonstrate the capsule writer output (markdown capsule with structured KPIs)
5. **Key point**: Every agent decision is traced, aggregated, and available for evolution analysis

---

## 11. Compliance (2 min)

**Goal**: Demonstrate GDPR right-to-delete and audit logging.

1. Navigate to the compliance admin panel
2. Trigger a right-to-delete request for a test customer
3. Show the deletion workflow: data identified -> anonymised -> audit log created
4. Show the data export endpoint (JSON download of customer data)
5. Show the consent management interface (consent records with timestamps)
6. **Key point**: Full GDPR Article 17 compliance with immutable audit trail

---

## 12. Marketplace (2 min)

**Goal**: Show the vendor ecosystem and commission engine.

1. Navigate to the marketplace admin panel
2. Show vendor onboarding: create a new vendor with business details
3. Assign commission rates (per-category and per-vendor)
4. Show a vendor's product listing and sales attribution
5. Show the payout tracking dashboard (pending + completed payouts)
6. **Key point**: Multi-vendor marketplace with configurable commission and automated payout tracking

---

## Demo Summary

| Section | Duration | Key Capability |
|---------|----------|---------------|
| 1. Onboarding | 2 min | AI-guided tenant setup |
| 2. Payments | 3 min | 4-provider gateway + webhooks |
| 3. Multi-channel | 3 min | 6-channel simultaneous listing |
| 4. Pricing | 3 min | AI + competitor + guardrails |
| 5. Customer service | 3 min | Bilingual auto-reply + escalation |
| 6. Margin dashboard | 3 min | ROI + dead-stock + commission |
| 7. MADRL | 2 min | Multi-agent conflict resolution |
| 8. OOM prevention | 2 min | Backpressure + self-recovery |
| 9. Cloud deploy | 3 min | Helm + KEDA + Terraform |
| 10. Agentrace | 2 min | Agent observability + EvoMap |
| 11. Compliance | 2 min | GDPR + audit + export |
| 12. Marketplace | 2 min | Vendor + commission + payout |
| **Total** | **30 min** | |
