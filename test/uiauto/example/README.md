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
