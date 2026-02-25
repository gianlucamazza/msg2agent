# msg2agent Infrastructure (Terraform)

This directory contains the Terraform configuration to deploy the `msg2agent` relay service on Google Cloud Platform using modern best practices.

## Architecture

*   **Compute**: Managed Instance Group (MIG) running Container-Optimized OS.
    *   **Self-Healing**: If the VM fails, it is automatically recreated.
    *   **Persistence**: A "Stateful Disk" is attached. Data in `/data` (SQLite DB) allows the VM to be replaced without data loss.
*   **Networking**:
    *   **Global Load Balancer**: Provides a single public Anycast IP.
    *   **Managed SSL**: Google-managed certificate for `msg2agent.xyz`.
    *   **Firewall**: Restricts VM access to only Load Balancer traffic (secure).
*   **Security**:
    *   Dedicated Service Account with minimal permissions (Logging, Monitoring, Artifact Registry).

## Prerequisites

1.  **Terraform**: Installed (v1.0+).
2.  **GCloud SDK**: Authenticated (`gcloud auth application-default login`).
3.  **Project ID**: You need the project ID (`message2agent`).

## Usage

1.  **Initialize Terraform**:
    ```bash
    terraform init
    ```

2.  **Preview Changes**:
    ```bash
    terraform plan -var="project_id=message2agent"
    ```

3.  **Apply Deployment**:
    ```bash
    terraform apply -var="project_id=message2agent"
    ```

## Post-Deployment

*   Update your Domain DNS to point `msg2agent.xyz` to the IP output by Terraform (`lb_ip`).
*   Wait for SSL provisioning (can take 15-30 mins).

## Inputs (variables.tf)

| Name | Description | Default |
|------|-------------|---------|
| `project_id` | GCP Project ID | **Required** |
| `region` | GCP Region | `europe-west1` |
| `image_tag` | Docker Image Tag | `latest` |
| `domain_name` | Domain for SSL | `msg2agent.xyz` |

## State Management

The persistent disk `msg2agent-data` is marked as **Stateful**.
*   **Safe**: `terraform apply` updates will NOT delete this disk.
*   **Danger**: `terraform destroy` WILL delete the disk and data.
