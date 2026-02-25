variable "project_id" {
  description = "The Google Cloud Project ID"
  type        = string
}

variable "region" {
  description = "The Google Cloud Region"
  type        = string
  default     = "europe-west1"
}

variable "zone" {
  description = "The Google Cloud Zone"
  type        = string
  default     = "europe-west1-b"
}

variable "image_tag" {
  description = "The Docker image tag to deploy"
  type        = string
  default     = "latest"
}

variable "domain_name" {
  description = "The domain name for the service (managed SSL)"
  type        = string
  default     = "msg2agent.xyz"
}

variable "repo_name" {
  description = "Artifact Registry Repository Name"
  type        = string
  default     = "msg2agent-repo"
}

variable "service_account_id" {
  description = "Service Account ID for the Relay"
  type        = string
  default     = "msg2agent-relay-sa"
}
