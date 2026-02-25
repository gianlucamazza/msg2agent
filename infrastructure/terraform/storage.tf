# Artifact Registry Repository
resource "google_artifact_registry_repository" "repo" {
  location      = var.region
  repository_id = var.repo_name
  description   = "msg2agent container repository"
  format        = "DOCKER"
}

# GCS Bucket for SQLite Persistence
resource "google_storage_bucket" "data_store" {
  name     = "msg2agent-data-${var.project_id}"
  location = var.region
  
  uniform_bucket_level_access = true
  
  # Prevent accidental deletion of production data
  force_destroy = false
}
