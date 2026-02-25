# Container Optimized OS Image
data "google_compute_image" "cos" {
  family  = "cos-stable"
  project = "cos-cloud"
}

# Instance Template
resource "google_compute_instance_template" "relay_template" {
  name_prefix  = "msg2agent-relay-template-"
  machine_type = "e2-small"
  
  region = var.region

  # Boot Disk
  disk {
    source_image = data.google_compute_image.cos.self_link
    auto_delete  = true
    boot         = true
    disk_size_gb = 10
    disk_type    = "pd-balanced"
  }

  # Data Disk (will be stateful)
  disk {
    mode         = "READ_WRITE"
    disk_size_gb = 10
    disk_type    = "pd-balanced"
    device_name  = "msg2agent-data"
    type         = "PERSISTENT"
    auto_delete  = false
  }

  network_interface {
    network = "default"
    access_config {
      # Ephemeral public IP
    }
  }

  tags = ["msg2agent-relay"]

  service_account {
    email  = google_service_account.relay_sa.email
    scopes = ["cloud-platform"]
  }

  metadata = {
    user-data = templatefile("${path.module}/cloud-config.yaml", {
      relay_image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.repo_name}/relay:${var.image_tag}"
      agent_image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.repo_name}/agent:${var.image_tag}"
    })
    google-logging-enabled = "true"
  }

  lifecycle {
    create_before_destroy = true
  }
}

# Zonal Managed Instance Group
resource "google_compute_instance_group_manager" "relay_mig" {
  name               = "msg2agent-relay-mig"
  base_instance_name = "msg2agent-relay"
  zone               = var.zone
  target_size        = 1

  version {
    instance_template = google_compute_instance_template.relay_template.id
  }

  named_port {
    name = "http"
    port = 8081
  }

  # Stateful Disk
  stateful_disk {
    device_name = "msg2agent-data"
    delete_rule = "NEVER"
  }

  auto_healing_policies {
    health_check      = google_compute_health_check.relay_hc.id
    initial_delay_sec = 300
  }

  update_policy {
    type                  = "PROACTIVE"
    replacement_method    = "RECREATE"
    minimal_action        = "REPLACE"
    max_surge_fixed       = 0
    max_unavailable_fixed = 1
  }
}
