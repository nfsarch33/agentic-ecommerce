output "deployment_summary" {
  description = "Credential-free GCP Cloud Run deployment summary for dry-run review."
  value = {
    provider      = local.provider_name
    project_id    = var.gcp_project_id
    region        = var.gcp_region
    network_id    = module.network.network_id
    vpc_connector = module.network.vpc_connector_name
    postgres = {
      engine     = module.postgres.engine_label
      instance   = module.postgres.instance_name
      secret_ref = module.postgres.connection_secret_ref
      migrations = module.postgres.migration_command
    }
    redis = {
      engine     = module.redis.engine_label
      instance   = module.redis.instance_name
      secret_ref = module.redis.endpoint_secret_ref
    }
    media_store = module.media_store.deployment_contract
    services = {
      mc_api          = module.mc_api_service.deployment_contract
      wc_sync         = module.wc_sync_service.deployment_contract
      agent_worker    = module.agent_worker_service.deployment_contract
      temporal_server = module.temporal_server_service.deployment_contract
      temporal_worker = module.temporal_worker_service.deployment_contract
      frontend        = module.frontend_service.deployment_contract
    }
  }
}
