-- v2.4.0 — Marketplace plugin framework + tenant aggregate (down).
-- Forward-only migrations; this file is provided for local dev resets.

DROP TABLE IF EXISTS marketplace_event_subscriptions;
DROP TABLE IF EXISTS marketplace_installations;
DROP TABLE IF EXISTS marketplace_plugins;
DROP TABLE IF EXISTS tenants;
