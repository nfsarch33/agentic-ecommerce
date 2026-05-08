package testsupportpg

// CanonicalMigrationFiles returns the ordered DDL applied to every
// ephemeral test container. The list mirrors the migrate-up Make target
// so adapter and rag integration tests run against the same schema as
// production. Adding a new migration is a one-line append; tests pick
// it up automatically via StartPool.
func CanonicalMigrationFiles() []string {
	return []string{
		"0001_create_products.up.sql",
		"0002_create_orders.up.sql",
		"0003_create_product_media_assets.up.sql",
		"0004_add_tenant_id.up.sql",
		"0005_enable_pgvector_rag.up.sql",
		"0006_tenant_settings_compliance_reporting.up.sql",
		"0007_membership.up.sql",
		"0008_digital.up.sql",
		"0009_marketplace.up.sql",
		"0010_billing.up.sql",
		"0011_rls.up.sql",
	}
}
