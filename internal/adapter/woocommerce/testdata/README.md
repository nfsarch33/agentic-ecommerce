# WooCommerce Adapter Fixtures

These deterministic `httptest` fixtures exercise the WooCommerce REST adapter without requiring live store credentials. A future cassette pass should replay the same scenarios against a disposable WooCommerce compose store and scrub all auth query parameters before committing recordings.
