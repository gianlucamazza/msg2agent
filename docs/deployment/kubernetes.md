# Kubernetes Deployment

This guide covers deploying msg2agent components on Kubernetes.

## Prerequisites

- Kubernetes cluster (1.25+)
- kubectl configured
- Container images pushed to a registry

## Relay Deployment

### ConfigMap

```yaml
# relay-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: relay-config
data:
  MSG2AGENT_LOG_LEVEL: "info"
  MSG2AGENT_MAX_CONNECTIONS: "1000"
  MSG2AGENT_STORE: "sqlite"
  MSG2AGENT_STORE_FILE: "/data/relay.db"
```

### Deployment

```yaml
# relay-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: relay
  labels:
    app: msg2agent
    component: relay
spec:
  replicas: 1
  selector:
    matchLabels:
      app: msg2agent
      component: relay
  template:
    metadata:
      labels:
        app: msg2agent
        component: relay
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: relay
          image: msg2agent/relay:latest
          args:
            - "-addr"
            - ":8080"
          ports:
            - name: ws
              containerPort: 8080
              protocol: TCP
          envFrom:
            - configMapRef:
                name: relay-config
          resources:
            requests:
              memory: "64Mi"
              cpu: "100m"
            limits:
              memory: "256Mi"
              cpu: "500m"
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
          volumeMounts:
            - name: data
              mountPath: /data
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: relay-pvc
```

### PersistentVolumeClaim

```yaml
# relay-pvc.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: relay-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
```

### Service

```yaml
# relay-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: relay
  labels:
    app: msg2agent
    component: relay
spec:
  type: ClusterIP
  ports:
    - port: 8080
      targetPort: 8080
      protocol: TCP
      name: ws
  selector:
    app: msg2agent
    component: relay
```

### Ingress (Optional)

```yaml
# relay-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: relay
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
    nginx.ingress.kubernetes.io/websocket-services: "relay"
spec:
  rules:
    - host: relay.msg2agent.xyz
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: relay
                port:
                  number: 8080
```

## Agent Deployment

### ConfigMap

```yaml
# agent-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-config
data:
  MSG2AGENT_DOMAIN: "msg2agent.xyz"
  MSG2AGENT_RELAY: "ws://relay:8080"
  MSG2AGENT_LOG_LEVEL: "info"
```

### Deployment

```yaml
# agent-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-alice
  labels:
    app: msg2agent
    component: agent
    agent: alice
spec:
  replicas: 1
  selector:
    matchLabels:
      app: msg2agent
      component: agent
      agent: alice
  template:
    metadata:
      labels:
        app: msg2agent
        component: agent
        agent: alice
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: agent
          image: msg2agent/agent:latest
          args:
            - "-name"
            - "alice"
            - "-http"
            - ":8081"
            - "-metrics"
            - ":9090"
          ports:
            - name: http
              containerPort: 8081
              protocol: TCP
            - name: metrics
              containerPort: 9090
              protocol: TCP
          envFrom:
            - configMapRef:
                name: agent-config
          resources:
            requests:
              memory: "64Mi"
              cpu: "100m"
            limits:
              memory: "256Mi"
              cpu: "500m"
          livenessProbe:
            httpGet:
              path: /health
              port: 8081
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /health
              port: 8081
            initialDelaySeconds: 5
            periodSeconds: 5
```

### Service

```yaml
# agent-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: agent-alice
  labels:
    app: msg2agent
    component: agent
    agent: alice
spec:
  type: ClusterIP
  ports:
    - port: 8081
      targetPort: 8081
      protocol: TCP
      name: http
    - port: 9090
      targetPort: 9090
      protocol: TCP
      name: metrics
  selector:
    app: msg2agent
    component: agent
    agent: alice
```

## TLS Configuration

### Using cert-manager

```yaml
# certificate.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: relay-tls
spec:
  secretName: relay-tls-secret
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
    - relay.msg2agent.xyz
```

### Mounting TLS Secrets

```yaml
# In the deployment spec:
spec:
  containers:
    - name: relay
      args:
        - "-addr"
        - ":8443"
        - "-tls"
        - "-tls-cert"
        - "/certs/tls.crt"
        - "-tls-key"
        - "/certs/tls.key"
      volumeMounts:
        - name: tls
          mountPath: /certs
          readOnly: true
  volumes:
    - name: tls
      secret:
        secretName: relay-tls-secret
```

## Applying Manifests

```bash
# Create namespace
kubectl create namespace msg2agent

# Apply configs
kubectl apply -n msg2agent -f relay-config.yaml
kubectl apply -n msg2agent -f agent-config.yaml

# Apply storage
kubectl apply -n msg2agent -f relay-pvc.yaml

# Apply deployments
kubectl apply -n msg2agent -f relay-deployment.yaml
kubectl apply -n msg2agent -f relay-service.yaml
kubectl apply -n msg2agent -f agent-deployment.yaml
kubectl apply -n msg2agent -f agent-service.yaml

# Check status
kubectl get pods -n msg2agent
kubectl get svc -n msg2agent
```

## Monitoring with Prometheus

Add the following ServiceMonitor for Prometheus Operator:

```yaml
# servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: msg2agent
  labels:
    app: msg2agent
spec:
  selector:
    matchLabels:
      app: msg2agent
  endpoints:
    - port: metrics
      interval: 15s
```

## Scaling Considerations

- The relay is designed for single-instance deployment with SQLite
- For high availability, use an external database (future feature)
- Agents can be scaled horizontally; each agent has a unique DID
- Use pod anti-affinity for spreading agents across nodes

## Further Reading

- [Configuration Guide](../operations/configuration.md) - All environment variables and flags
- [Monitoring Guide](../operations/monitoring.md) - Prometheus and Grafana setup
- [TLS Setup](tls-setup.md) - Certificate generation and configuration
