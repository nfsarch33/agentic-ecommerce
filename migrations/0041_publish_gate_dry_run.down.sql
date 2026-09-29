-- Restore the 0040 write_kind CHECK (dry-run rows must be removed first).
ALTER TABLE publish_ledger DROP CONSTRAINT publish_ledger_write_kind_check;
ALTER TABLE publish_ledger ADD CONSTRAINT publish_ledger_write_kind_check
    CHECK (write_kind IN ('create','update','none'));
