# Network setup
resource "google_compute_global_address" "default" {
  name = "msg2agent-lb-ip"
}

# Firewall rule to allow Google Load Balancer health checks
resource "google_compute_firewall" "allow_health_check" {
  name    = "allow-health-check"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["80"]
  }

  source_ranges = ["130.211.0.0/22", "35.191.0.0/16"]
  target_tags   = ["msg2agent-relay"]
}
