# Temporal Workflow Conventions

> Last verified: 2026-05-11

Standardized in v5.4.0. All Temporal workflows and activities in
`internal/workflow/` follow these patterns.

## Activity Naming

**Constant naming**: `<Domain><Action>Activity` (Go identifier).

**String value**: `<domain>.<action>` (stable across replay; NEVER
change a string value without a migration plan).

Examples:

| Go constant                        | String value                       |
|------------------------------------|------------------------------------|
| TenantValidateRegistrationActivity | `tenant.validate_registration`     |
| NormaliseChannelOrderActivity      | `order_aggregator.normalise`       |
| ValidateReturnEligibilityActivity  | `returns_saga.validate_eligibility`|
| SelectPaymentProviderActivity      | `payment_saga.select_provider`     |
| MembershipChargeStripeActivity     | `membership.charge_stripe`         |
| MediaSourceActivity                | `media_processing.source`          |
| ContentGenerateActivity            | `content_generation.generate`      |
| CheckComplianceActivity            | `product_publish.check_compliance` |
| VendorVerifyActivity               | `vendor.verify`                    |
| SearchSuppliersActivity            | `sourcing.search_suppliers`        |

## Timeout Configuration

All workflows apply activity options at the top of the workflow
function via `temporalworkflow.WithActivityOptions`. Two standard
profiles:

### Standard HTTP-calling activities

```go
temporalworkflow.ActivityOptions{
    StartToCloseTimeout: time.Minute,
    RetryPolicy: &temporal.RetryPolicy{
        InitialInterval:    time.Second,
        BackoffCoefficient: 2,
        MaximumInterval:    30 * time.Second,
        MaximumAttempts:    3,
    },
}
```

Used by: order_aggregator, returns_saga, sourcing, membership_lifecycle,
media_processing, content_generation, product_publish.

### Local / idempotent activities

```go
temporalworkflow.ActivityOptions{
    StartToCloseTimeout: 30 * time.Second,
    RetryPolicy: &temporal.RetryPolicy{
        InitialInterval:    time.Second,
        BackoffCoefficient: 2,
        MaximumInterval:    15 * time.Second,
        MaximumAttempts:    3,
    },
}
```

Used by: tenant_onboarding, vendor_onboarding (all activities are
local DB operations).

### Payment saga (single-attempt for idempotency)

```go
temporalworkflow.ActivityOptions{
    StartToCloseTimeout: time.Minute,
    RetryPolicy: &temporal.RetryPolicy{
        InitialInterval:    time.Second,
        BackoffCoefficient: 2,
        MaximumInterval:    30 * time.Second,
        MaximumAttempts:    1,
    },
}
```

The payment saga handles retries at the workflow level
(`chargeWithRetry` loop) to maintain idempotency control.

## Determinism Rules

Workflow functions MUST NOT:

1. Call `time.Now()` -- use `temporalworkflow.Now(ctx)`.
2. Use `math/rand` -- use `workflow.SideEffect`.
3. Make direct HTTP calls -- wrap in an activity.
4. Iterate maps in control-flow-affecting order.
5. Use goroutines -- use `workflow.Go`.

### Audit results (v5.4.0)

All 10 workflow functions audited. Zero determinism violations found.
All workflows correctly use `temporalworkflow.Now(ctx)` for timestamps
and route all I/O through activities.

Workflows audited:
- `TenantOnboardingWorkflow`
- `OrderAggregatorWorkflow`
- `ReturnsSagaWorkflow`
- `PaymentSagaWorkflow`
- `VendorOnboardingWorkflow`
- `SourcingWorkflow`
- `MembershipLifecycleWorkflow`
- `MediaProcessingWorkflow`
- `ContentGenerationWorkflow`
- `ProductPublishWorkflow`

## Workflow Inventory

| Workflow                     | File                         | Activities | Saga? |
|------------------------------|------------------------------|-----------|-------|
| TenantOnboardingWorkflow     | tenant_onboarding.go         | 6         | Yes   |
| OrderAggregatorWorkflow      | order_aggregator.go          | 3         | No    |
| ReturnsSagaWorkflow          | returns_saga.go              | 11        | Yes   |
| PaymentSagaWorkflow          | payment_saga.go              | 6         | Yes   |
| VendorOnboardingWorkflow     | vendor_onboarding.go         | 5         | Yes   |
| SourcingWorkflow             | sourcing.go                  | 5         | No    |
| MembershipLifecycleWorkflow  | membership_lifecycle.go      | 3         | No    |
| MediaProcessingWorkflow      | media_processing.go          | 5         | No    |
| ContentGenerationWorkflow    | content_generation.go        | 4         | No    |
| ProductPublishWorkflow       | product_publish.go           | 4         | No    |
