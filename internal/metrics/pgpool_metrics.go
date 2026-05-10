package metrics

// registerPGPoolMetrics wires the v5.5.0 Postgres connection pool
// metrics into the registry.
func registerPGPoolMetrics(r *Registry) {
	r.PGPoolOpenConnections = newGauge(r, "ec_pg_pool_open_connections", "v5.5.0 Postgres pool: total open connections.")
	r.PGPoolIdleConnections = newGauge(r, "ec_pg_pool_idle_connections", "v5.5.0 Postgres pool: idle connections.")
	r.PGPoolWaitTotal = newCounter(r, "ec_pg_pool_wait_total", "v5.5.0 Postgres pool: total connection-acquire waits.")
	r.PGPoolWaitDuration = newHistogram(r, "ec_pg_pool_wait_duration_seconds", "v5.5.0 Postgres pool: connection-acquire wait duration histogram.", defaultDurationBuckets)
}
