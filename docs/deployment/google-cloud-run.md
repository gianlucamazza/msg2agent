# Deploying to Google Cloud Run

This guide explains how to deploy the `msg2agent` Relay Hub to Google Cloud Run using the provided automation script.

## Prerequisites

1.  **Google Cloud SDK**: Ensure `gcloud` is installed and authenticated.
2.  **Docker**: Ensure Docker is running locally.
3.  **GCP Project**: You need a Google Cloud project with billing enabled.

## Required APIs

Enable the following APIs in your Google Cloud project:

```bash
gcloud services enable artifactregistry.googleapis.com run.googleapis.com cloudbuild.googleapis.com
```

## Deployment

We provide a helper script `scripts/deploy-cloud-run.sh` that automates the entire process:

1.  Creates an Artifact Registry repository (if missing).
2.  Builds the Relay Docker image (`linux/amd64`).
3.  Pushes the image to the registry.
4.  Deploys the service to Cloud Run.

### Usage

Run the script from the project root:

```bash
# Syntax: ./scripts/deploy-cloud-run.sh <PROJECT_ID> [REGION]
./scripts/deploy-cloud-run.sh message2agent europe-west1
```

### Configuration Details

The script deploys with the following configuration:

- **Authentication**: `--allow-unauthenticated` (Publicly accessible).
  - _Note_: Security is handled by the application layer (DID cryptographic signatures and encryption), not the transport layer.
- **Storage**: `MSG2AGENT_STORE=memory` (Stateless).
  - _Warning_: Data is lost on container restart. For production persistence, configure Cloud SQL or Filestore.
- **Port**: 8080

## Verification

After deployment, the script outputs the Service URL (e.g., `https://msg2agent-relay-xyz.a.run.app`).

### 1. Test Connectivity

Use `wscat` or `curl` to verify the service is up:

```bash
# Check health
curl https://msg2agent-relay-xyz.a.run.app/health
# Expected: ok
```

### 2. Connect Agents

Start local agents pointing to the Cloud Run Relay:

```bash
# Agent Alice
./agent -name alice -relay wss://msg2agent-relay-prod-568831106201.europe-west1.run.app

# Agent Bob
./agent -name bob -relay wss://msg2agent-relay-prod-568831106201.europe-west1.run.app
```

Note the use of `wss://` (WebSocket Secure) since Cloud Run provides HTTPS by default.

## Production Setup (Best Practices)

For a production-ready environment with **data persistence** and **security hardening**, use the `deploy-prod.sh` script.

### Features

- **Persistence**: Uses Cloud Storage FUSE to mount a GCS bucket at `/data`. The SQLite database is stored here, ensuring agent registrations survive restarts.
- **Security**: Creates a dedicated Service Account (`msg2agent-relay-sa`) with minimal permissions (only access to the specific storage bucket), instead of using the default Compute Engine account.

### Usage

```bash
# Syntax: ./scripts/deploy-prod.sh <PROJECT_ID> [REGION]
./scripts/deploy-prod.sh message2agent europe-west1
```

### Accessing in Production

If your organization enforces `Domain Restricted Sharing`, the production service will not be publicly accessible even with `--allow-unauthenticated`.
**Best Practice**: Configure an **Internal Application Load Balancer (ALB)** to expose the service securely.

## Public Access via Load Balancer

To expose the service publicly (bypassing organization restrictions), we use an **Application Load Balancer (ALB)**.

We provide a script to automate the LB creation: `scripts/deploy-lb.sh`.

### What it does

1.  Reserves a **Global Static IP**.
2.  Creates a **Serverless NEG** for the Cloud Run service.
3.  Sets up a **Backend Service**, **URL Map**, and **Target Proxy**.
4.  Creates a **Global Forwarding Rule** acting as the frontend.

### Usage

```bash
# Syntax: ./scripts/deploy-lb.sh <PROJECT_ID> [REGION]
./scripts/deploy-lb.sh message2agent europe-west1
```

### Validation

The script will output the Global IP Address.
Wait 5-10 minutes for the LB to provision, then test:
`curl http://<GLOBAL_IP>/health`

## SSL and Custom Domain

To secure the service with HTTPS and a custom domain (`msg2agent.xyz`), use the `setup-ssl.sh` script.

### Prerequisites

1.  **DNS A Record**: Point `msg2agent.xyz` to the Global IP Address of the Load Balancer (`136.110.209.218`).
2.  **Wait**: Ensure DNS propagation logic has started.

### Usage

```bash
# Syntax: ./scripts/setup-ssl.sh <DOMAIN> <PROJECT_ID>
./scripts/setup-ssl.sh msg2agent.xyz message2agent
```

### What it does

1.  Creates a **Google-managed SSL Certificate** for the domain.
2.  Sets up an **HTTPS Target Proxy** and **Forwarding Rule** (Port 443).
3.  Configures an **HTTP-to-HTTPS Redirect** automatically.

### Verification

The certificate status will be `PROVISIONING` for 15-60 minutes while Google validates the DNS.
Once `ACTIVE`, your service will be available at `https://msg2agent.xyz`.
