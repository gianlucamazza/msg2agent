#!/bin/bash
set -e

# Configuration
PROJECT_ID=$1
REGION=${2:-europe-west1}
SERVICE_NAME="msg2agent-relay-prod"
LB_NAME="msg2agent-lb"

if [ -z "$PROJECT_ID" ]; then
    echo "Usage: $0 <PROJECT_ID> [REGION]"
    echo "Example: $0 my-gcp-project-id europe-west1"
    exit 1
fi

echo "========================================================"
echo "Deploying Load Balancer for $SERVICE_NAME"
echo "Project: $PROJECT_ID"
echo "Region:  $REGION"
echo "========================================================"

# 1. Reserve Global Static IP
IP_NAME="${LB_NAME}-ip"
echo "[1/6] Reserving Global Static IP '$IP_NAME'..."
if ! gcloud compute addresses describe $IP_NAME --global --project $PROJECT_ID >/dev/null 2>&1; then
    gcloud compute addresses create $IP_NAME --global --project $PROJECT_ID
else
    echo "IP '$IP_NAME' already exists."
fi
IP_ADDRESS=$(gcloud compute addresses describe $IP_NAME --global --project $PROJECT_ID --format="value(address)")
echo "Reserved IP: $IP_ADDRESS"

# 2. Create Serverless NEG
NEG_NAME="${LB_NAME}-neg"
echo "[2/6] Creating Serverless NEG '$NEG_NAME'..."
if ! gcloud compute network-endpoint-groups describe $NEG_NAME --region $REGION --project $PROJECT_ID >/dev/null 2>&1; then
    gcloud compute network-endpoint-groups create $NEG_NAME \
        --region=$REGION \
        --network-endpoint-type=serverless \
        --cloud-run-service=$SERVICE_NAME \
        --project=$PROJECT_ID
else
    echo "NEG '$NEG_NAME' already exists."
fi

# 3. Create Backend Service
BACKEND_NAME="${LB_NAME}-backend"
echo "[3/6] Creating Backend Service '$BACKEND_NAME'..."
if ! gcloud compute backend-services describe $BACKEND_NAME --global --project $PROJECT_ID >/dev/null 2>&1; then
    gcloud compute backend-services create $BACKEND_NAME \
        --global \
        --project=$PROJECT_ID

    gcloud compute backend-services add-backend $BACKEND_NAME \
        --global \
        --network-endpoint-group=$NEG_NAME \
        --network-endpoint-group-region=$REGION \
        --project=$PROJECT_ID
else
    echo "Backend Service '$BACKEND_NAME' already exists."
fi

# 4. Create URL Map
URL_MAP_NAME="${LB_NAME}-url-map"
echo "[4/6] Creating URL Map '$URL_MAP_NAME'..."
if ! gcloud compute url-maps describe $URL_MAP_NAME --global --project $PROJECT_ID >/dev/null 2>&1; then
    gcloud compute url-maps create $URL_MAP_NAME \
        --default-service=$BACKEND_NAME \
        --global \
        --project=$PROJECT_ID
else
    echo "URL Map '$URL_MAP_NAME' already exists."
fi

# 5. Create Target HTTP Proxy
PROXY_NAME="${LB_NAME}-http-proxy"
echo "[5/6] Creating Target HTTP Proxy '$PROXY_NAME'..."
if ! gcloud compute target-http-proxies describe $PROXY_NAME --global --project $PROJECT_ID >/dev/null 2>&1; then
    gcloud compute target-http-proxies create $PROXY_NAME \
        --url-map=$URL_MAP_NAME \
        --global \
        --project=$PROJECT_ID
else
    echo "Target Proxy '$PROXY_NAME' already exists."
fi

# 6. Create Global Forwarding Rule
FWD_RULE_NAME="${LB_NAME}-fwd-rule"
echo "[6/6] Creating Forwarding Rule '$FWD_RULE_NAME'..."
if ! gcloud compute forwarding-rules describe $FWD_RULE_NAME --global --project $PROJECT_ID >/dev/null 2>&1; then
    gcloud compute forwarding-rules create $FWD_RULE_NAME \
        --target-http-proxy=$PROXY_NAME \
        --global \
        --ports=80 \
        --address=$IP_NAME \
        --project=$PROJECT_ID
else
    echo "Forwarding Rule '$FWD_RULE_NAME' already exists."
fi

# 7. Final Instructions
echo "========================================================"
echo "Load Balancer Deployed Successfully!"
echo "Global IP Address: $IP_ADDRESS"
echo ""
echo "NEXT STEPS:"
echo "1. Wait 5-10 minutes for the Load Balancer to propagate."
echo "2. Visit http://$IP_ADDRESS/health to verify."
echo "3. (Optional) Point your domain DNS (A Record) to $IP_ADDRESS"
echo "   and enable Google-managed SSL certificates."
echo "========================================================"
