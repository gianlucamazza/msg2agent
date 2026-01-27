#!/bin/bash
set -e

# Configuration
PROJECT_ID=$1
REGION=${2:-europe-west1}
REPO_NAME="msg2agent-repo"
IMAGE_NAME="relay"
SERVICE_NAME="msg2agent-relay-prod"
BUCKET_NAME="msg2agent-data-${PROJECT_ID}"
SA_NAME="msg2agent-relay-sa"

# Check arguments
if [ -z "$PROJECT_ID" ]; then
    echo "Usage: $0 <PROJECT_ID> [REGION]"
    echo "Example: $0 my-gcp-project-id europe-west1"
    exit 1
fi

echo "========================================================"
echo "Deploying msg2agent Relay (PRODUCTION) to Google Cloud Run"
echo "Project: $PROJECT_ID"
echo "Region:  $REGION"
echo "Bucket:  $BUCKET_NAME"
echo "========================================================"

# 1. Ensure APIs are enabled (Critical for GCS FUSE)
echo "[1/7] Enabling required APIs..."
gcloud services enable \
    artifactregistry.googleapis.com \
    run.googleapis.com \
    cloudbuild.googleapis.com \
    storage.googleapis.com \
    iam.googleapis.com \
    --project $PROJECT_ID

# 2. Create Service Account for Least Privilege
echo "[2/7] Checking Service Account..."
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
if ! gcloud iam service-accounts describe $SA_EMAIL --project $PROJECT_ID >/dev/null 2>&1; then
    echo "Creating Service Account '$SA_NAME'..."
    gcloud iam service-accounts create $SA_NAME \
        --display-name="msg2agent Relay Service Account" \
        --project $PROJECT_ID
else
    echo "Service Account '$SA_NAME' already exists."
fi

# 3. Create GCS Bucket for Persistence
echo "[3/7] Checking GCS Bucket..."
if ! gcloud storage buckets describe gs://$BUCKET_NAME --project $PROJECT_ID >/dev/null 2>&1; then
    echo "Creating bucket '$BUCKET_NAME'..."
    gcloud storage buckets create gs://$BUCKET_NAME --location=$REGION --project=$PROJECT_ID
else
    echo "Bucket '$BUCKET_NAME' already exists."
fi

# 4. Grant Permissions to Service Account
echo "[4/7] Granting storage permissions..."
# Grant Storage Object Admin to the SA on the bucket
gcloud storage buckets add-iam-policy-binding gs://$BUCKET_NAME \
    --member="serviceAccount:$SA_EMAIL" \
    --role="roles/storage.objectAdmin" \
    --project $PROJECT_ID >/dev/null

# 5. Build Image (if needed, reusing existing Logic)
echo "[5/7] Building/Pushing Image..."
# We reuse the artifact registry setup from the dev script logic
ARTIFACT_REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}"
IMAGE_URI="${ARTIFACT_REGISTRY}/${IMAGE_NAME}:latest"

if ! gcloud artifacts repositories describe $REPO_NAME --location=$REGION --project=$PROJECT_ID >/dev/null 2>&1; then
     gcloud artifacts repositories create $REPO_NAME \
        --repository-format=docker \
        --location=$REGION \
        --description="msg2agent container repository" \
        --project=$PROJECT_ID
fi

gcloud auth configure-docker ${REGION}-docker.pkg.dev --quiet
docker build --target relay --platform linux/amd64 -t $IMAGE_URI .
docker push $IMAGE_URI

# 6. Deploy to Cloud Run with Volume Mount
echo "[6/7] Deploying to Cloud Run with GCS FUSE..."
# Note: execution-environment=gen2 is recommended for file system operations
gcloud run deploy $SERVICE_NAME \
    --image $IMAGE_URI \
    --region $REGION \
    --project $PROJECT_ID \
    --platform managed \
    --service-account $SA_EMAIL \
    --execution-environment gen2 \
    --allow-unauthenticated \
    --port 8080 \
    --add-volume name=relay-data,type=cloud-storage,bucket=$BUCKET_NAME \
    --add-volume-mount volume=relay-data,mount-path=/data \
    --set-env-vars MSG2AGENT_STORE=sqlite \
    --set-env-vars MSG2AGENT_STORE_FILE=/data/relay.db \
    --set-env-vars MSG2AGENT_LOG_LEVEL=info

# 7. Final Output
echo "========================================================"
echo "Production Deployment Complete!"
echo "Service URL:"
URL=$(gcloud run services describe $SERVICE_NAME --platform managed --region $REGION --project $PROJECT_ID --format 'value(status.url)')
echo $URL
echo ""
echo "NOTE on Access:"
echo "If you get a 403 Forbidden, your organization restricts external access."
echo "You should map this service to an Internal Application Load Balancer."
echo "========================================================"
