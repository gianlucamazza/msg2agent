#!/bin/bash
set -e

# Configuration
DOMAIN=$1
PROJECT_ID=$2
LB_NAME="msg2agent-lb"

if [ -z "$DOMAIN" ] || [ -z "$PROJECT_ID" ]; then
	echo "Usage: $0 <DOMAIN_NAME> <PROJECT_ID>"
	echo "Example: $0 msg2agent.xyz message2agent"
	exit 1
fi

echo "========================================================"
echo "Setting up SSL for Domain: $DOMAIN"
echo "Project: $PROJECT_ID"
echo "Load Balancer: $LB_NAME"
echo "========================================================"

# 1. Create Managed SSL Certificate
CERT_NAME="msg2agent-ssl-cert"

echo "[1/4] Creating Managed SSL Certificate '$CERT_NAME'..."
if ! gcloud compute ssl-certificates describe $CERT_NAME --global --project $PROJECT_ID >/dev/null 2>&1; then
	gcloud compute ssl-certificates create $CERT_NAME \
		--domains=$DOMAIN \
		--global \
		--project=$PROJECT_ID
else
	echo "Certificate '$CERT_NAME' already exists."
fi

# 2. Create Target HTTPS Proxy
HTTPS_PROXY_NAME="${LB_NAME}-https-proxy"
URL_MAP_NAME="${LB_NAME}-url-map" # Assumes created by deploy-lb.sh

echo "[2/4] Creating Target HTTPS Proxy '$HTTPS_PROXY_NAME'..."
if ! gcloud compute target-https-proxies describe $HTTPS_PROXY_NAME --global --project $PROJECT_ID >/dev/null 2>&1; then
	gcloud compute target-https-proxies create $HTTPS_PROXY_NAME \
		--ssl-certificates=$CERT_NAME \
		--url-map=$URL_MAP_NAME \
		--global \
		--project=$PROJECT_ID
else
	echo "HTTPS Proxy '$HTTPS_PROXY_NAME' already exists."
fi

# 3. Create Global Forwarding Rule for HTTPS (Port 443)
HTTPS_FWD_RULE="${LB_NAME}-https-fwd-rule"
IP_NAME="${LB_NAME}-ip" # Must match deploy-lb.sh
IP_ADDRESS=$(gcloud compute addresses describe $IP_NAME --global --project $PROJECT_ID --format="value(address)")

echo "[3/4] Creating Forwarding Rule '$HTTPS_FWD_RULE' on IP $IP_ADDRESS..."
if ! gcloud compute forwarding-rules describe $HTTPS_FWD_RULE --global --project $PROJECT_ID >/dev/null 2>&1; then
	gcloud compute forwarding-rules create $HTTPS_FWD_RULE \
		--target-https-proxy=$HTTPS_PROXY_NAME \
		--global \
		--ports=443 \
		--address=$IP_NAME \
		--project=$PROJECT_ID
else
	echo "Forwarding Rule '$HTTPS_FWD_RULE' already exists."
fi

# 4. Configure HTTP-to-HTTPS Redirect
# To do this cleanly, we create a specialized URL Map that redirects everything
REDIRECT_MAP_NAME="${LB_NAME}-http-redirect-map"
HTTP_PROXY_NAME="${LB_NAME}-http-proxy" # Must match deploy-lb.sh

echo "[4/4] Configuring HTTP-to-HTTPS Redirect..."

# Import URL Map from a simple yaml definition
cat <<EOF >redirect-map.yaml
kind: compute#urlMap
name: $REDIRECT_MAP_NAME
defaultUrlRedirect:
  httpsRedirect: true
  redirectResponseCode: MOVED_PERMANENTLY_DEFAULT
EOF

if ! gcloud compute url-maps describe $REDIRECT_MAP_NAME --global --project $PROJECT_ID >/dev/null 2>&1; then
	gcloud compute url-maps import $REDIRECT_MAP_NAME \
		--source=redirect-map.yaml \
		--global \
		--project=$PROJECT_ID
else
	# Update if exists to ensure config is correct
	gcloud compute url-maps import $REDIRECT_MAP_NAME \
		--source=redirect-map.yaml \
		--global \
		--project=$PROJECT_ID --quiet
fi
rm redirect-map.yaml

# Update HTTP Proxy to use the redirect map
echo "Updating HTTP Proxy '$HTTP_PROXY_NAME' to use Redirect Map..."
gcloud compute target-http-proxies update $HTTP_PROXY_NAME \
	--url-map=$REDIRECT_MAP_NAME \
	--global \
	--project=$PROJECT_ID

echo "========================================================"
echo "SSL Setup Completed!"
echo "Domain: https://$DOMAIN"
echo ""
echo "IMPORTANT:"
echo "1. Ensure DNS A record for $DOMAIN points to $IP_ADDRESS"
echo "2. Certificate Status: $(gcloud compute ssl-certificates describe $CERT_NAME --global --project $PROJECT_ID --format='value(managed.status)')"
echo "   It may take 15-60 minutes significantly to reach 'ACTIVE' status after DNS propagation."
echo "========================================================"
