# Secret Manager secrets for msg2agent production credentials.
# Values are populated manually via:
#   gcloud secrets versions add <secret-name> --data-file=- <<< "value"

locals {
  secret_names = [
    "stripe-secret-key",
    "stripe-webhook-secret",
    "stripe-price-free",
    "stripe-price-starter",
    "stripe-price-team",
    "stripe-price-enterprise",
    "msg2agent-service-token",
  ]
}

resource "google_secret_manager_secret" "msg2agent_secrets" {
  for_each = toset(local.secret_names)

  secret_id = each.value
  project   = var.project_id

  replication {
    auto {}
  }
}

# Grant the relay service account access to all msg2agent secrets.
resource "google_secret_manager_secret_iam_member" "relay_sa_secret_access" {
  for_each = toset(local.secret_names)

  project   = var.project_id
  secret_id = google_secret_manager_secret.msg2agent_secrets[each.value].secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.relay_sa.email}"
}
