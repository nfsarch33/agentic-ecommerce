locals {
  create_network = var.network == ""
  network_name   = local.create_network ? google_compute_network.vpc[0].self_link : var.network
  subnet_name    = local.create_network ? google_compute_subnetwork.subnet[0].self_link : var.subnetwork
}

resource "google_compute_network" "vpc" {
  count = local.create_network ? 1 : 0

  name                    = "${var.cluster_name}-vpc"
  auto_create_subnetworks = false
  project                 = var.project_id
}

resource "google_compute_subnetwork" "subnet" {
  count = local.create_network ? 1 : 0

  name          = "${var.cluster_name}-subnet"
  ip_cidr_range = "10.0.0.0/20"
  region        = var.region
  network       = google_compute_network.vpc[0].id
  project       = var.project_id

  secondary_ip_range {
    range_name    = "${var.cluster_name}-pods"
    ip_cidr_range = var.pods_cidr_range
  }

  secondary_ip_range {
    range_name    = "${var.cluster_name}-services"
    ip_cidr_range = var.services_cidr_range
  }

  private_ip_google_access = true
}
