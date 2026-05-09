# China Adapter Cassettes

These cassettes are deterministic local mock recordings created with
`gopkg.in/dnaeon/go-vcr.v3`. They exercise the same JSON-over-HTTP adapter
contract used by the 1688 and Taobao clients without scraping real accounts,
storing cookies, or depending on interactive marketplace login.

Live marketplace smoke remains operator-run only because 1688 and Taobao
sessions require credentials and may trigger interactive anti-bot checks.
When a human operator has approved the session and provided local-only cookies,
run the live adapter smoke from an isolated checkout:

```bash
ECOMMERCE_1688_SESSION_COOKIE='<redacted>' \
ECOMMERCE_TAOBAO_SESSION_COOKIE='<redacted>' \
go test -tags=live ./internal/adapter/china -run 'TestLive(1688|Taobao)SearchSmokeRequiresOperatorSession' -count=1 -v
```

Do not commit live cookies, HTML captures, account IDs, or marketplace
responses containing personal or supplier-private data.
