# Health Check
resource "google_compute_health_check" "relay_hc" {
  name                = "msg2agent-relay-hc"
  check_interval_sec  = 10
  timeout_sec         = 5
  healthy_threshold   = 2
  unhealthy_threshold = 3

  http_health_check {
    port         = 8081
    request_path = "/health"
  }
}

# Backend Service
resource "google_compute_backend_service" "relay_backend" {
  name        = "msg2agent-relay-backend"
  port_name   = "http"
  protocol    = "HTTP"
  timeout_sec = 30
  
  # Enable Cloud CDN (Optional, good for static assets but Relay is API mostly)
  enable_cdn  = false

  health_checks = [google_compute_health_check.relay_hc.id]

  backend {
    group = google_compute_instance_group_manager.relay_mig.instance_group
  }
}

# URL Map (Load Balancer)
resource "google_compute_url_map" "default" {
  name            = "msg2agent-lb"
  default_service = google_compute_backend_service.relay_backend.id
}

# HTTP → HTTPS Redirect URL Map
resource "google_compute_url_map" "http_redirect" {
  name = "msg2agent-http-redirect"

  default_url_redirect {
    https_redirect         = true
    strip_query            = false
    redirect_response_code = "MOVED_PERMANENTLY_DEFAULT"
  }
}

# Managed SSL Certificate
resource "google_compute_managed_ssl_certificate" "default" {
  name = "msg2agent-ssl-cert"

  managed {
    domains = [var.domain_name]
  }
}

# HTTP Proxy (Redirect URL Map - usually we'd have a separate one for HTTPS redirect)
resource "google_compute_target_http_proxy" "default" {
  name    = "msg2agent-http-proxy"
  url_map = google_compute_url_map.http_redirect.id
}

resource "google_compute_global_forwarding_rule" "http" {
  name       = "msg2agent-lb-fwd-rule"
  target     = google_compute_target_http_proxy.default.id
  port_range = "80"
  ip_address = google_compute_global_address.default.address
}

# HTTPS Proxy
resource "google_compute_target_https_proxy" "default" {
  name             = "msg2agent-https-proxy"
  url_map          = google_compute_url_map.default.id
  ssl_certificates = [google_compute_managed_ssl_certificate.default.id]
}

resource "google_compute_global_forwarding_rule" "https" {
  name       = "msg2agent-lb-https-fwd-rule"
  target     = google_compute_target_https_proxy.default.id
  port_range = "443"
  ip_address = google_compute_global_address.default.address
}
