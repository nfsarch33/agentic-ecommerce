# Local n8n Automation

v1.5.0 adds an opt-in, self-hosted n8n service for local automation workflow
development. The backend outbound webhook bridge and registration API are owned
by a separate backend slice; this runbook only covers compose wiring, local n8n
state, and importable example workflows.

For the v2.0.0 outbound registration API, event envelope, HMAC headers, and
inbound WooCommerce webhook boundary, see `docs/webhook-contracts.md`.

## Start n8n

Copy the relevant example environment file, keep `BIND_HOST=127.0.0.1`, then
start only the n8n profile:

```bash
cp .env.example .env
make n8n-config
make n8n-up
```

Open `http://127.0.0.1:${N8N_HOST_PORT:-5678}` and complete n8n's local owner
setup in your browser. Stop n8n while preserving its named volume:

```bash
make n8n-down
```

The dev stack stores n8n state in the `ec-n8ndata` named volume. The
production-like compose file uses `n8n-data`. Neither volume is committed to git.

## Configuration Placeholders

The compose files intentionally expose placeholders only:

```bash
N8N_IMAGE_TAG=1.88.0
N8N_HOST_PORT=5678
N8N_HOST=127.0.0.1
N8N_PROTOCOL=http
N8N_EDITOR_BASE_URL=http://127.0.0.1:5678/
N8N_WEBHOOK_URL=http://127.0.0.1:5678/
N8N_ENCRYPTION_KEY=
GENERIC_TIMEZONE=UTC
N8N_SLACK_WEBHOOK_URL=
N8N_ORDER_EMAIL_ENDPOINT_URL=
```

Set `N8N_ENCRYPTION_KEY` only in your untracked local environment before saving
real n8n credentials. Set `N8N_SLACK_WEBHOOK_URL` or
`N8N_ORDER_EMAIL_ENDPOINT_URL` only when you intentionally test against a local
or approved provider endpoint. Do not commit `.env`, `.env.compose`, Slack
incoming webhook URLs, SMTP credentials, transactional email tokens, or exported
n8n credentials.

## Import Workflow Templates

Validate the checked-in templates:

```bash
make n8n-workflows-validate
```

Then import either JSON file from the n8n UI:

- `deploy/n8n/workflows/product-approved-slack-notification.json`
- `deploy/n8n/workflows/order-placed-email-confirmation.json`

Both templates are inactive by default, contain no `credentials` entries, and
acknowledge the backend webhook request through a Respond to Webhook node. The
checked-in validator also verifies that each Webhook trigger is connected, each
HTTP Request node uses an environment placeholder URL, and no live provider URLs
or tokens are present.

After import, inspect the generated local webhook URL in n8n before registering
it with the backend. The template paths are:

- `product-approved`
- `order-placed`

The Slack example posts to `{{$env.SLACK_WEBHOOK_URL}}`, populated in compose by
`N8N_SLACK_WEBHOOK_URL`. The order confirmation example posts to
`{{$env.ORDER_EMAIL_ENDPOINT_URL}}`, populated by
`N8N_ORDER_EMAIL_ENDPOINT_URL`. During QA, point those variables only at local
mock receivers such as `httptest`, `nc`-style local listeners, or a local SMTP
bridge. Do not use Slack, SMTP, n8n Cloud, or other credentialed destinations.

After the backend webhook bridge lands, register the active n8n webhook URLs
there instead of hard-coding backend or provider endpoints into workflow JSON.
Use a per-registration secret and keep it in your local environment or secret
manager; the backend API returns only `secret_hash` and never echoes the raw
secret.

## Security Boundaries

- Published n8n ports bind to loopback by default through
  `BIND_HOST=127.0.0.1`.
- The n8n container does not mount the Docker socket.
- `security_opt: no-new-privileges:true` is enabled in both compose files.
- Execution data defaults avoid saving successful executions and progress data.
- Workflow templates must remain inactive and credential-free in git.
- Provider credentials belong in n8n's encrypted credential store or local
  environment, never in committed workflow exports.

Before opening a PR, run:

```bash
make n8n-config
runx shell-leak-scan --repo ecommerce
sentrux gate .
```
