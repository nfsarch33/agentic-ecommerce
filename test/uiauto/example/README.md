# uiauto example smoke scenario

Bundled with the backend repo so `make uiauto-smoke` works without a checkout
of `agentic-ecommerce-web`. Reuses the canonical `example.com` smoke that
`uiauto-framework` ships in
`examples/example-com-smoke/scenario.json` (cited from
`~/Code/personal/uiauto-framework/examples/example-com-smoke/scenario.json`),
so the smoke gate stays honest about whether the docker compose runner
can attach to chromedp at all.

The real comparison fixtures live in
`agentic-ecommerce-web/test/uiauto/scenarios/`; the backend's
`uiauto-runner` compose service mounts that directory at
`/work/scenarios:ro` via `UIAUTO_SCENARIOS_PATH`, while this directory
mounts at `/work/example-scenarios:ro` via
`UIAUTO_EXAMPLE_SCENARIOS_PATH` (see `docker-compose.dev.yml`).

## Listing verification

After publish, the listing itself is verified rather than trusted: a
uiauto-framework nightly scenario (`listing-verify`, in that repo's
`eval/suites/nightly-regression.yaml`) opens the live listing page, checks
title, price, image count and stock against the publishgate record, and
writes a PASS or FAIL row carrying a screenshot path into the nightly
ledger — a page that times out is a FAIL naming the reason, never a silent
pass. The record this repo owns is
`test/uiauto/fixtures/uiauto/listing-record.json`: the gate's written
fields for the fixture listing, mirrored verbatim in the scenario's
`verify` block so the nightly measures the verification, not a hand-copied
expectation.
