locals {
  merged_labels = merge(
    {
      app         = "agentic-ecommerce"
      environment = var.environment
      managed_by  = "terraform"
    },
    var.labels,
  )
}

resource "google_container_cluster" "autopilot" {
  name     = var.cluster_name
  location = var.region
  project  = var.project_id

  enable_autopilot = true

  network    = local.network_name
  subnetwork = local.subnet_name

  ip_allocation_policy {
    cluster_secondary_range_name  = "${var.cluster_name}-pods"
    services_secondary_range_name = "${var.cluster_name}-services"
  }

  release_channel {
    channel = "REGULAR"
  }

  resource_labels = local.merged_labels

  deletion_protection = false
}

resource "google_sql_database_instance" "postgres" {
  name             = "${var.cluster_name}-db"
  database_version = var.db_version
  region           = var.region
  project          = var.project_id

  settings {
    tier              = var.db_tier
    availability_type = "REGIONAL"

    backup_configuration {
      enabled                        = true
      point_in_time_recovery_enabled = true
      start_time                     = "03:00"
    }

    ip_configuration {
      ipv4_enabled    = false
      private_network = local.network_name
    }

    database_flags {
      name  = "max_connections"
      value = "200"
    }

    user_labels = local.merged_labels
  }

  deletion_protection = false
}

resource "google_sql_database" "ecommerce" {
  name     = var.db_name
  instance = google_sql_database_instance.postgres.name
  project  = var.project_id
}

resource "google_redis_instance" "cache" {
  name           = "${var.cluster_name}-redis"
  tier           = "STANDARD_HA"
  memory_size_gb = var.redis_memory_size_gb
  region         = var.region
  project        = var.project_id

  redis_version = var.redis_version

  authorized_network = local.network_name

  labels = local.merged_labels
}
