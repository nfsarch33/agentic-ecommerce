-- v6.1.0 CF-15: rollback webhook_idempotency table.

DROP INDEX IF EXISTS idx_webhook_idempotency_reserved_at;
DROP TABLE IF EXISTS webhook_idempotency;
