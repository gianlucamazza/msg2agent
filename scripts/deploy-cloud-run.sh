#!/bin/bash
set -e

# Configuration
PROJECT_ID=$1
REGION=${2:-us-central1}
REPO_NAME="msg2agent-repo"
IMAGE_NAME="relay"
SERVICE_NAME="msg2agent-relay"

# Check arguments
if [ -z "$PROJECT_ID" ]; then
    echo "Usage: $0 <PROJECT_ID> [REGION]"
    echo "Example: $0 my-gcp-project-id europe-west1"
    exit 1
fi

echo "========================================================"
echo "Deploying msg2agent Relay to Google Cloud Run"
echo "Project: $PROJECT_ID"
echo "Region:  $REGION"
echo "========================================================"

# 1. Create Artifact Registry Repository if it doesn't exist
echo "[1/5] Checking Artifact Registry repository..."
if ! gcloud artifacts repositories describe $REPO_NAME --location=$REGION --project=$PROJECT_ID >/dev/null 2>&1; then
    echo "Creating repository '$REPO_NAME'..."
    gcloud artifacts repositories create $REPO_NAME \
        --repository-format=docker \
        --location=$REGION \
        --description="msg2agent container repository" \
        --project=$PROJECT_ID
else
    echo "Repository '$REPO_NAME' already exists."
fi

# 2. Configure Docker authentication
echo "[2/5] Configuring Docker authentication..."
gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet

# 3. Build Relay Image
IMAGE_URI="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}/${IMAGE_NAME}:latest"
echo "[3/5] Building Docker image..."
# Note: targeting 'relay' stage specifically
docker build --target relay --platform linux/amd64 -t $IMAGE_URI .

# 4. Push Image
echo "[4/5] Pushing image to Artifact Registry..."
docker push $IMAGE_URI

# 5. Deploy to Cloud Run
echo "[5/5] Deploying service to Cloud Run..."
# We use --allow-unauthenticated because encryption/auth is handled at application layer (DIDs)
# We set store to memory for stateless operation (persistence requires Cloud SQL or Volumes)
gcloud run deploy $SERVICE_NAME \
    --image $IMAGE_URI \
    --region $REGION \
    --project $PROJECT_ID \
    --platform managed \
    --allow-unauthenticated \
    --port 8080 \
    --set-env-vars MSG2AGENT_STORE=memory \
    --set-env-vars MSG2AGENT_LOG_LEVEL=info

echo "========================================================"
echo "Deployment Complete!"
echo "Service URL:"
gcloud run services describe $SERVICE_NAME --platform managed --region $REGION --project $PROJECT_ID --format 'value(status.url)'
echo "========================================================"
