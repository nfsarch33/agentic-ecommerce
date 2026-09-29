-- 0040 limited ledger write_kind to create|update|none, but the gate's
-- dry-run completion writes write_kind='dry_run' (design D7): every publish
-- under ECOMMERCE_PUBLISH_MODE=dry-run violated the CHECK. Extend it here
-- rather than editing the applied 0040.
ALTER TABLE publish_ledger DROP CONSTRAINT publish_ledger_write_kind_check;
ALTER TABLE publish_ledger ADD CONSTRAINT publish_ledger_write_kind_check
    CHECK (write_kind IN ('create','update','none','dry_run'));
