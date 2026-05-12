# Social Sandbox Readiness -- v7.5.1 QA refresh

Status: BLOCKED for live social sandbox execution. Default CI uses replay
cassettes, `httptest.Server` fixtures, and injected `BaseURL` values only. No
live social API call is part of default tests.

## Decision

The six social channels stay operator-gated for live execution:

- tiktok
- facebook
- rednote
- woocommerce
- instagram
- pinterest

Live social API work requires approved app credentials, store/account access,
budget, and runx-safe secret loading. Do not run live social adapter tests on
the MacBook unless those prerequisites are explicitly met.

## Mock / Live Boundary Matrix

| Channel | Default runtime boundary | Hermetic CI coverage | Live sandbox gate |
| --- | --- | --- | --- |
| tiktok | Defaults to the TikTok API root unless tests inject `BaseURL`. | `internal/adapter/social/tiktok_shop_client_test.go` uses replay cassettes and `httptest.Server`-style fixtures. | Future `live_social_sandbox`, operator-gated. |
| facebook | Defaults to the Meta Graph API root unless tests inject `BaseURL`. | `internal/adapter/social/facebook_shop_client_test.go` uses replay cassettes and local fixtures. | Future `live_social_sandbox`, operator-gated. |
| rednote | uiauto/channel bridge remains local-fixture driven by default. | `internal/adapter/social/adapter_factory_test.go` validates credential shape; channel behavior is covered through higher-level adapter tests. | Future `live_social_sandbox`, operator-gated. |
| woocommerce | Channel factory validation is local; WooCommerce live store credentials are external. | `internal/adapter/social/adapter_factory_test.go` validates the channel credential gate. | Future `live_social_sandbox`, operator-gated. |
| instagram | Production adapter defaults to `DefaultInstagramBaseURL`; tests inject `BaseURL` and `HTTPClient`. | `internal/adapter/social/instagram_shop_test.go` uses `httptest.Server`. | Future `live_social_sandbox`, operator-gated. |
| pinterest | Production adapter defaults to `DefaultPinterestBaseURL`; tests inject `BaseURL` and `HTTPClient`. | `internal/adapter/social/pinterest_shop_test.go` uses `httptest.Server`. | Future `live_social_sandbox`, operator-gated. |

## Credential Handling

All live social credentials must enter through approved runx or operator-vault
surfaces. Never put access tokens, app secrets, browser profiles, or merchant
account details on argv, in committed fixtures, or in default CI.

## Validation

Default validation stays local:

```text
go test ./internal/adapter/social -count=1
go test ./tests/quality -run TestV751 -count=1
```

Live validation is intentionally absent until an operator provisions account
access and approves a build-tagged live test path.

