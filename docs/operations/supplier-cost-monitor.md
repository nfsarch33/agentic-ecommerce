# Supplier Cost Monitor Contract

The supplier cost monitor emits `SupplierCostChangedEvent` only when the
absolute relative delta is **strictly greater than** the configured threshold.

For the default 5% threshold:

| Baseline | Observed | Delta | Event |
|---:|---:|---:|---|
| 1000 | 1049 | 4.9% | no |
| 1000 | 1050 | 5.0% | no |
| 1000 | 1051 | 5.1% | yes |

This matches the shipped implementation in `internal/monitor/supplier_cost.go`
(`abs(delta) <= threshold` stays inside the noise band) and the validation
matrix in `tests/integration/v351/supplier_cost_event_validation_test.go`.

## Why The Boundary Is Exclusive

The monitor treats the configured threshold as the inclusive edge of acceptable
supplier-cost noise. A 5.0% move at a 5.0% threshold is still inside the band;
5.1% crosses it. This avoids repeated events for rounding-boundary movements
while still catching material supplier changes.

## Regression Surface

- `internal/monitor/supplier_cost_test.go` pins 4%, 5%, and 5.1% cases.
- `tests/integration/v351/supplier_cost_event_validation_test.go` pins the
  end-to-end event payload and metric behavior for scenario 3.
