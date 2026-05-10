-- Reverse v3.8.0 EC-7-3 shipping_labels table.
DROP INDEX IF EXISTS idx_shipping_labels_tenant_status;
DROP INDEX IF EXISTS idx_shipping_labels_tenant_order;
DROP TABLE IF EXISTS shipping_labels;
