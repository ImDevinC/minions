# Kubernetes Deployment Guide

Deploy minions to a Kubernetes cluster. This guide walks through namespace setup, secrets, network policies, and service deployments.

## Prerequisites

- Kubernetes 1.27+ (for Pod Security Standards)
- `kubectl` configured with cluster access
- PostgreSQL database accessible from cluster
- Container images built and pushed to registry

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     minions namespace                        │
│                                                              │
│  ┌──────────────┐      ┌─────────────────┐                  │
│  │ orchestrator │◄────►│   discord-bot   │                  │
│  │    :8080     │      │     :8081       │                  │
│  └──────────────┘      └─────────────────┘                  │
│         │                                                    │
│         ▼                                                    │
│  ┌──────────────────────────────────────┐                   │
│  │         devbox pods (ephemeral)       │                   │
│  │  app=minion-devbox   :4096 (SSE)      │                   │
│  └──────────────────────────────────────┘                   │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## 1. Namespace Setup

Create the `minions` namespace with Pod Security Standards enforcement:

```yaml
# infra/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: minions
  labels:
    # Baseline profile: blocks known privilege escalations
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/enforce-version: latest
    # Warn on restricted violations (helps catch issues early)
    pod-security.kubernetes.io/warn: restricted
    pod-security.kubernetes.io/warn-version: latest
```

Apply:

```bash
kubectl apply -f infra/namespace.yaml
```

### Pod Security Standards

The namespace uses two levels of Pod Security Standards:

| Level | Mode | Effect |
|-------|------|--------|
| `baseline` | `enforce` | Blocks pods violating baseline policies (privilege escalation, etc.) |
| `restricted` | `warn` | Logs warnings for stricter violations (helps you tighten security) |

See [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/) for details.

## 2. RBAC Configuration

The orchestrator needs permission to manage devbox pods:

```yaml
# infra/rbac.yaml

# ServiceAccount for orchestrator
apiVersion: v1
kind: ServiceAccount
metadata:
  name: minions-orchestrator
  namespace: minions
---
# Role: pod management within minions namespace
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: minions-pod-manager
  namespace: minions
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["create", "delete", "get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
---
# Bind Role to ServiceAccount
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: minions-orchestrator-binding
  namespace: minions
subjects:
  - kind: ServiceAccount
    name: minions-orchestrator
    namespace: minions
roleRef:
  kind: Role
  name: minions-pod-manager
  apiGroup: rbac.authorization.k8s.io
```

Apply:

```bash
kubectl apply -f infra/rbac.yaml
```

## 3. Secrets Configuration

Create secrets for all credentials. **Do not commit actual values to git.**

### Option A: Create from kubectl (recommended)

```bash
# Database
kubectl create secret generic minions-db -n minions \
  --from-literal=DATABASE_URL='postgres://user:pass@host:5432/minions?sslmode=require'

# LLM API keys (OpenRouter)
kubectl create secret generic minions-llm-keys -n minions \
  --from-literal=OPENROUTER_API_KEY='sk-or-...'

# GitHub App (use --from-file for PEM key)
kubectl create secret generic minions-github-app -n minions \
  --from-literal=GITHUB_APP_ID='123456' \
  --from-file=GITHUB_APP_PRIVATE_KEY=./private-key.pem

# Discord bot
kubectl create secret generic minions-discord-bot -n minions \
  --from-literal=DISCORD_BOT_TOKEN='MTIz...'

# Internal API token (generate with: openssl rand -hex 32)
kubectl create secret generic minions-internal-api -n minions \
  --from-literal=INTERNAL_API_TOKEN="$(openssl rand -hex 32)"
```

### Option B: Template manifests

For GitOps workflows, use template manifests and replace values in CI:

```yaml
# infra/secrets.yaml (template - REPLACE_ME values)
apiVersion: v1
kind: Secret
metadata:
  name: minions-db
  namespace: minions
type: Opaque
stringData:
  DATABASE_URL: "REPLACE_ME"
---
apiVersion: v1
kind: Secret
metadata:
  name: minions-llm-keys
  namespace: minions
type: Opaque
stringData:
  OPENROUTER_API_KEY: "REPLACE_ME"
---
apiVersion: v1
kind: Secret
metadata:
  name: minions-github-app
  namespace: minions
type: Opaque
stringData:
  GITHUB_APP_ID: "REPLACE_ME"
  GITHUB_APP_PRIVATE_KEY: |
    -----BEGIN RSA PRIVATE KEY-----
    REPLACE_WITH_YOUR_PRIVATE_KEY
    -----END RSA PRIVATE KEY-----
---
apiVersion: v1
kind: Secret
metadata:
  name: minions-discord-bot
  namespace: minions
type: Opaque
stringData:
  DISCORD_BOT_TOKEN: "REPLACE_ME"
---
apiVersion: v1
kind: Secret
metadata:
  name: minions-internal-api
  namespace: minions
type: Opaque
stringData:
  INTERNAL_API_TOKEN: "REPLACE_ME"
```

### Option C: External Secrets Operator

For production, consider [External Secrets Operator](https://external-secrets.io/) to sync from AWS Secrets Manager, Vault, etc.

### Verify secrets

```bash
kubectl get secrets -n minions
# NAME                   TYPE     DATA   AGE
# minions-db             Opaque   1      1m
# minions-llm-keys       Opaque   1      1m
# minions-github-app     Opaque   2      1m
# minions-discord-bot    Opaque   1      1m
# minions-internal-api   Opaque   1      1m
```

## 4. Network Policies

Restrict traffic between components (defense in depth):

### Devbox Pods

Devbox pods run user-provided tasks, so they're locked down hard:

```yaml
# Egress: DNS, HTTPS (GitHub/LLM APIs), orchestrator callback
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: devbox-egress
  namespace: minions
spec:
  podSelector:
    matchLabels:
      app: minion-devbox
  policyTypes:
    - Egress
  egress:
    # DNS
    - to:
        - namespaceSelector: {}
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
    # HTTPS (external APIs)
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 10.0.0.0/8
              - 172.16.0.0/12
              - 192.168.0.0/16
      ports:
        - protocol: TCP
          port: 443
    # Orchestrator callback
    - to:
        - podSelector:
            matchLabels:
              app: minions-orchestrator
      ports:
        - protocol: TCP
          port: 8080
---
# Ingress: deny all
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: devbox-deny-ingress
  namespace: minions
spec:
  podSelector:
    matchLabels:
      app: minion-devbox
  policyTypes:
    - Ingress
  ingress: []
```

### Orchestrator & Discord Bot

See `infra/network-policies.yaml` for the full manifest, including:

- Orchestrator ingress from devbox, control panel, ingress controller
- Orchestrator egress to K8s API, PostgreSQL, Discord bot, GitHub, devbox SSE
- Discord bot egress to Discord API/Gateway, orchestrator
- Discord bot ingress from orchestrator webhooks

Apply:

```bash
kubectl apply -f infra/network-policies.yaml
```

## 5. Deployments

### Orchestrator

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orchestrator
  namespace: minions
spec:
  replicas: 1
  strategy:
    type: Recreate  # single writer to DB
  selector:
    matchLabels:
      app.kubernetes.io/name: orchestrator
  template:
    metadata:
      labels:
        app.kubernetes.io/name: orchestrator
    spec:
      serviceAccountName: minions-orchestrator
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
      containers:
        - name: orchestrator
          image: ghcr.io/imdevinc/minions/orchestrator:latest
          ports:
            - name: http
              containerPort: 8080
          env:
            - name: PORT
              value: "8080"
            - name: DISCORD_BOT_WEBHOOK_URL
              value: "http://discord-bot.minions.svc.cluster.local:8081/webhook"
          envFrom:
            - secretRef:
                name: minions-db
            - secretRef:
                name: minions-internal-api
            - secretRef:
                name: minions-llm-keys
            - secretRef:
                name: minions-github-app
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 512Mi
          readinessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 10
            periodSeconds: 30
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
---
apiVersion: v1
kind: Service
metadata:
  name: orchestrator
  namespace: minions
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: orchestrator
  ports:
    - name: http
      port: 8080
      targetPort: http
```

### Discord Bot

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: discord-bot
  namespace: minions
spec:
  replicas: 1
  strategy:
    type: Recreate  # Discord gateway doesn't support dual connections
  selector:
    matchLabels:
      app.kubernetes.io/name: discord-bot
  template:
    metadata:
      labels:
        app.kubernetes.io/name: discord-bot
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
      containers:
        - name: discord-bot
          image: ghcr.io/imdevinc/minions/discord-bot:latest
          ports:
            - name: http
              containerPort: 8081
          env:
            - name: PORT
              value: "8081"
            - name: ORCHESTRATOR_URL
              value: "http://orchestrator.minions.svc.cluster.local:8080"
          envFrom:
            - secretRef:
                name: minions-discord-bot
            - secretRef:
                name: minions-internal-api
            - secretRef:
                name: minions-llm-keys
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 256Mi
          readinessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /health
              port: http
            initialDelaySeconds: 10
            periodSeconds: 30
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
---
apiVersion: v1
kind: Service
metadata:
  name: discord-bot
  namespace: minions
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: discord-bot
  ports:
    - name: http
      port: 8081
      targetPort: http
```

Apply:

```bash
kubectl apply -f infra/deployments.yaml
```

## 6. Verify Deployment

```bash
# Check pods
kubectl get pods -n minions
# NAME                            READY   STATUS    RESTARTS   AGE
# orchestrator-xxx                1/1     Running   0          1m
# discord-bot-xxx                 1/1     Running   0          1m

# Check services
kubectl get svc -n minions
# NAME           TYPE        CLUSTER-IP       PORT(S)
# orchestrator   ClusterIP   10.96.xxx.xxx    8080/TCP
# discord-bot    ClusterIP   10.96.xxx.xxx    8081/TCP

# Test health endpoints
kubectl run curl --rm -it --image=curlimages/curl --restart=Never -n minions -- \
  curl -s http://orchestrator:8080/health
# {"status":"ok"}

# Check logs
kubectl logs -n minions deployment/orchestrator -f
kubectl logs -n minions deployment/discord-bot -f
```

## 7. Expose Externally (Optional)

If control panel needs external access to orchestrator API:

### Ingress (nginx-ingress)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: orchestrator
  namespace: minions
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - minions-api.example.com
      secretName: orchestrator-tls
  rules:
    - host: minions-api.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: orchestrator
                port:
                  number: 8080
```

### Service (LoadBalancer)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: orchestrator-lb
  namespace: minions
spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: orchestrator
  ports:
    - port: 443
      targetPort: http
```

## Troubleshooting

### Pod stuck in Pending

```bash
kubectl describe pod -n minions <pod-name>
# Look for Events section
```

Common causes:
- Insufficient resources (requests/limits too high)
- Node selector/affinity not matching
- PodSecurityPolicy/Pod Security Standards violation

### Pod CrashLoopBackOff

```bash
kubectl logs -n minions <pod-name> --previous
```

Common causes:
- Missing environment variables
- Database connection failed
- Invalid credentials

### NetworkPolicy blocking traffic

```bash
# Test connectivity from a debug pod
kubectl run debug --rm -it --image=nicolaka/netshoot --restart=Never -n minions -- bash

# Inside debug pod
curl -v http://orchestrator:8080/health
nc -zv orchestrator 8080
```

### RBAC issues

```bash
kubectl auth can-i create pods -n minions --as=system:serviceaccount:minions:minions-orchestrator
# yes
```

## Security Checklist

- [ ] Pod Security Standards enforced on namespace
- [ ] All pods run as non-root (UID 1000)
- [ ] Read-only root filesystem enabled
- [ ] All capabilities dropped
- [ ] NetworkPolicies restrict traffic
- [ ] Secrets not committed to git
- [ ] INTERNAL_API_TOKEN is cryptographically random
- [ ] Container images are from trusted registry
