-- Exactly-once publish gate: durable approvals and the write ledger.
CREATE TABLE IF NOT EXISTS publish_approvals (
  workflow_id   text PRIMARY KEY,
  tenant_id     text NOT NULL,
  product_id    uuid NOT NULL,
  decision      text NOT NULL CHECK (decision IN ('approved','rejected')),
  actor         text NOT NULL,
  reason        text NOT NULL DEFAULT '',
  update_id     text NOT NULL,
  decided_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS publish_ledger (
  idem_key      text PRIMARY KEY,
  tenant_id     text NOT NULL,
  workflow_id   text NOT NULL REFERENCES publish_approvals(workflow_id),
  product_id    uuid NOT NULL,
  sku           text NOT NULL,
  fingerprint   text NOT NULL,
  status        text NOT NULL CHECK (status IN ('in_progress','completed','dry_run','failed')),
  remote_id     text,
  write_kind    text CHECK (write_kind IN ('create','update','none')),
  attempts      int  NOT NULL DEFAULT 0,
  lease_owner   text,
  lease_until   timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  completed_at  timestamptz
);

CREATE INDEX IF NOT EXISTS publish_ledger_workflow ON publish_ledger (workflow_id);

-- RLS per the 0011 pattern (tenant_id policy on both tables).
DO $do$
DECLARE target text;
BEGIN
    FOREACH target IN ARRAY ARRAY['publish_approvals','publish_ledger'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', target);
        EXECUTE format($f$
            CREATE POLICY tenant_isolation ON %I
            USING (current_tenant_setting() = '' OR tenant_id = current_tenant_setting())
            WITH CHECK (current_tenant_setting() = '' OR tenant_id = current_tenant_setting())
        $f$, target, target);
    END LOOP;
END
$do$;
