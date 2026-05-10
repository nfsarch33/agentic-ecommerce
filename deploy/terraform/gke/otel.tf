resource "google_project_service" "cloudtrace" {
  project = var.project_id
  service = "cloudtrace.googleapis.com"

  disable_on_destroy = false
}

resource "google_service_account" "otel_collector" {
  account_id   = "otel-collector"
  display_name = "OTel Collector Service Account"
  project      = var.project_id
}

resource "google_project_iam_member" "otel_cloudtrace_agent" {
  project = var.project_id
  role    = "roles/cloudtrace.agent"
  member  = "serviceAccount:${google_service_account.otel_collector.email}"
}

resource "google_project_iam_member" "otel_monitoring_writer" {
  project = var.project_id
  role    = "roles/monitoring.metricWriter"
  member  = "serviceAccount:${google_service_account.otel_collector.email}"
}

resource "google_service_account_iam_member" "otel_workload_identity" {
  service_account_id = google_service_account.otel_collector.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[${var.namespace}/agentic-ecommerce-otel]"
}
