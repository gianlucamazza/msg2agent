# Service Account
resource "google_service_account" "relay_sa" {
  account_id   = var.service_account_id
  display_name = "msg2agent Relay Service Account"
}

# Permissions
resource "google_project_iam_member" "artifact_registry_reader" {
  project = var.project_id
  role    = "roles/artifactregistry.reader"
  member  = "serviceAccount:${google_service_account.relay_sa.email}"
}

resource "google_project_iam_member" "logging_log_writer" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.relay_sa.email}"
}

resource "google_project_iam_member" "monitoring_metric_writer" {
  project = var.project_id
  role    = "roles/monitoring.metricWriter"
  member  = "serviceAccount:${google_service_account.relay_sa.email}"
}

# Grant Storage Object Admin for the bucket (if needed for backup/logs)
# The GCS FUSE mount or simple cp requires specific roles.
resource "google_project_iam_member" "storage_admin" {
  project = var.project_id
  role    = "roles/storage.objectAdmin"
  member  = "serviceAccount:${google_service_account.relay_sa.email}"
}
