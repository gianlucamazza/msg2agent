output "lb_ip" {
  description = "The public IP address of the Load Balancer"
  value       = google_compute_global_address.default.address
}

output "ssl_certificate_id" {
  description = "The ID of the managed SSL certificate"
  value       = google_compute_managed_ssl_certificate.default.id
}

output "mig_instance_group" {
  description = "The instance group url"
  value       = google_compute_instance_group_manager.relay_mig.instance_group
}
