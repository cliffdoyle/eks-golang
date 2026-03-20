# MovieVault — Event-Driven Microservices on Amazon EKS

A production-grade microservices application running on Kubernetes (Amazon EKS), demonstrating event-driven architecture with Apache Kafka, PostgreSQL persistence, and a static frontend hosted on S3.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [How Kafka Works in This System](#how-kafka-works-in-this-system)
3. [Project Structure](#project-structure)
4. [Services](#services)
5. [Kubernetes Manifests — What Each File Does](#kubernetes-manifests)
6. [Critical Manifest Fixes](#critical-manifest-fixes)
7. [Deployment Guide](#deployment-guide)
8. [Networking — How Traffic Reaches the Services](#networking)
9. [Frontend — S3 Static Website](#frontend)
10. [CORS — Why It Was Needed](#cors)
11. [Load Testing](#load-testing)
12. [Troubleshooting Reference](#troubleshooting-reference)

---

## Architecture Overview

```
Browser (S3 Frontend)
        |
        |  HTTP via NodePort
        |----------------------------------------------|
        |                                              |
  node_ip:32186                                  node_ip:32673
        |                                              |
+-------+----------------------------------------------+------+
|                    EKS Cluster (cliff-eks-cluster)          |
|                    Namespace: movievault                    |
|                                                             |
|  +---------------------+      +-------------------------+  |
|  |  Inventory Service  |      |     Order Service       |  |
|  |  Go · port 8080     |      |  Go · port 8081         |  |
|  |  2 replicas         |      |  2 replicas             |  |
|  |                     |      |                         |  |
|  |  PRODUCER ──────────+------+──► CONSUMER            |  |
|  |  publishes events   |      |   builds local cache    |  |
|  +----------+----------+      +----------+--------------+  |
|             |                            |                 |
|             v                            v                 |
|  +----------------------------------------------------------+  |
|  |              PostgreSQL (port 5432)                   |  |
|  |   inventory DB: movies table                         |  |
|  |   order DB:     orders + available_movies tables     |  |
|  +----------------------------------------------------------+  |
|                                                             |
|  +------------------+    +------------------------------+  |
|  |   Apache Kafka   |    |         Zookeeper            |  |
|  |   port 9092      |◄---+         port 2181            |  |
|  |   topic:         |    |   (Kafka's coordinator)      |  |
|  |   movie.events   |    +------------------------------+  |
|  +------------------+                                       |
+-------------------------------------------------------------+

Amazon ECR  ← Docker images pulled from here by the nodes
Amazon S3   ← Static HTML frontend served from here
```

Two EC2 nodes (EKS Auto Mode) host all pods. Kubernetes spreads replicas across both nodes for high availability.

---

## How Kafka Works in This System

### The core design principle

The Order service **never calls the Inventory API directly**. Instead, the two services communicate exclusively through Kafka events. This is called loose coupling — if Inventory goes down, Order keeps working because it has its own local cache built from past events.

### The event flow step by step

```
1. User adds a movie via Inventory API (POST /movies)
   --> Inventory saves to its PostgreSQL movies table
   --> Inventory publishes to Kafka topic "movie.events":
       {
         "event": "movie.created",
         "movie": { "id": 1, "title": "Inception", "stock": 50 }
       }

2. Order service Kafka consumer (running as a goroutine) receives the event
   --> Inserts into its local available_movies table
   --> Now Order knows about this movie WITHOUT calling Inventory

3. User places an order (POST /orders)
   --> Order service checks its LOCAL available_movies cache
   --> If movie exists and stock >= requested quantity: confirms order
   --> If stock = 0 or movie deleted: rejects order

4. Inventory updates stock (PUT /movies/:id)
   --> Publishes "movie.stock_updated" event
   --> Order service receives it, updates its cache
   --> If new stock = 0: automatically cancels all pending orders

5. Inventory deletes a movie (DELETE /movies/:id)
   --> Publishes "movie.deleted" event
   --> Order service removes from cache
   --> All pending orders for that movie get cancelled
```

### Three event types

| Event | Published when | Order service reaction |
|---|---|---|
| `movie.created` | New movie added | Add to available_movies cache |
| `movie.stock_updated` | Stock level changed | Update cache; cancel orders if stock = 0 |
| `movie.deleted` | Movie removed | Delete from cache; cancel pending orders |

### Why Zookeeper is required

Kafka cannot start without Zookeeper. Zookeeper is Kafka's manager — it tracks which brokers are alive, coordinates leader election, and stores topic metadata. They are always deployed as a pair. In this project:

- Zookeeper starts first
- An `initContainer` in the Kafka pod waits until Zookeeper port 2181 responds before starting Kafka
- Only after Kafka is Running do the application services start

---

## Project Structure

```
eks-golang/
├── movievault/
│   ├── inventory-service/
│   │   ├── main.go          # Go HTTP server + Kafka producer
│   │   ├── Dockerfile
│   │   ├── go.mod
│   │   └── go.sum
│   ├── order-service/
│   │   ├── main.go          # Go HTTP server + Kafka consumer
│   │   ├── Dockerfile
│   │   ├── go.mod
│   │   └── go.sum
│   └── k8s/
│       ├── 00-namespace.yaml
│       ├── 01-postgres.yaml
│       ├── 02-kafka.yaml
│       ├── 03-inventory-service.yaml
│       └── 04-order-service.yaml
├── scripts/
│   ├── common.sh            # Shared config (AWS account, region, cluster name)
│   ├── 01-setup-ecr.sh      # Create ECR repositories (run once)
│   ├── 02-build-push.sh     # Build Docker images and push to ECR
│   ├── 03-deploy.sh         # Apply manifests and rollout
│   ├── 04-tesrdown.sh       # Delete all resources to stop AWS charges
│   └── 05-deploy-ui.sh      # Upload HTML to S3
├── movievault-ui.html       # Single-file admin frontend
└── README.md
```

---

## Services

### Inventory Service (port 8080)

Manages the movie catalogue. Acts as a Kafka producer — every mutation publishes an event.

| Method | Path | Description | Kafka event |
|---|---|---|---|
| GET | /movies | List all movies | none |
| POST | /movies | Add a movie | `movie.created` |
| GET | /movies/:id | Get one movie | none |
| PUT | /movies/:id | Update stock | `movie.stock_updated` |
| DELETE | /movies/:id | Delete movie | `movie.deleted` |
| GET | /health | Liveness/readiness probe | none |

### Order Service (port 8081)

Manages customer orders. Acts as a Kafka consumer — never calls Inventory directly.

| Method | Path | Description |
|---|---|---|
| GET | /orders | List all orders |
| POST | /orders | Place an order (validates against Kafka cache) |
| GET | /movies | Show Kafka cache (proves no Inventory API call) |
| GET | /health | Liveness/readiness probe |

---

## Kubernetes Manifests

Every manifest file contains 4 required top-level fields:

```yaml
apiVersion: apps/v1   # which K8s API handles this object
kind: Deployment      # WHAT to create — K8s reads this and knows what to do
metadata:
  name: my-service    # the name you give it
  namespace: movievault
spec:                 # your instructions — what to run, how many, what image
```

When you run `kubectl apply -f file.yaml`, kubectl reads the `kind` field and sends an HTTP request to the correct EKS API endpoint. You never specify what to do — the `kind` field does that automatically.

Multiple objects in one file are separated by `---`. kubectl processes each one independently in order.

### 00-namespace.yaml

Creates the `movievault` namespace — a logical boundary grouping all project resources. Deleting the namespace deletes everything inside it instantly.

### 01-postgres.yaml

Three objects: a ConfigMap for non-secret config, a Secret for the password, a Deployment running `postgres:16-alpine`, and a headless Service (`ClusterIP: None`) so pods connect directly by DNS name.

### 02-kafka.yaml

Four objects: Zookeeper Deployment and Service, Kafka Deployment and Service. The Kafka Deployment contains all the critical fixes described below.

### 03-inventory-service.yaml and 04-order-service.yaml

Each contains a Deployment (2 replicas, ECR image, env vars, health probes, resource limits) and a LoadBalancer Service with fixed NodePort values.

---

## Critical Manifest Fixes

These are the issues discovered during deployment and the exact changes that fixed them.

### Fix 1 — EKS Auto Mode tolerations

**Problem:** All pods stuck in `Pending` with the error:
```
0/3 nodes are available: 3 node(s) had untolerated taint(s)
```

**Root cause:** EKS Auto Mode taints every node with `eks.amazonaws.com/compute-type=auto:NoSchedule`. Without a matching toleration, the scheduler refuses to place any pod on any node — permanently.

**Fix:** Add to `spec.template.spec` of every Deployment:

```yaml
tolerations:
  - key: "eks.amazonaws.com/compute-type"
    operator: "Equal"
    value: "auto"
    effect: "NoSchedule"
```

Required on: zookeeper, kafka, inventory-service, order-service. Postgres had this in the original manifest which is why it was the only Running pod initially.

---

### Fix 2 — Kafka `enableServiceLinks: false`

**Problem:** Kafka crashed with exit code 1 within 1 second of starting. Logs showed only a deprecation warning then nothing.

**Root cause:** Kubernetes automatically injects an environment variable called `KAFKA_PORT` into the Kafka pod itself (because a Service named `kafka` exists in the namespace). Confluent's startup script detected this and tried to use it as the port configuration, corrupting the listener setup and crashing immediately.

**Fix:** Add to the Kafka pod spec:

```yaml
spec:
  enableServiceLinks: false
```

This stops Kubernetes injecting Service environment variables into this pod. It is the correct fix for any Confluent Kafka deployment on Kubernetes.

---

### Fix 3 — Kafka `initContainer` for Zookeeper readiness

**Problem:** Kafka crash-looped because it tried to connect to Zookeeper before Zookeeper was ready to accept connections.

**Fix:** Add an initContainer that blocks Kafka startup until Zookeeper is responsive:

```yaml
initContainers:
  - name: wait-for-zookeeper
    image: busybox:1.36
    command:
      - sh
      - -c
      - |
        until nc -z zookeeper.movievault.svc.cluster.local 2181; do
          echo "Waiting for Zookeeper..."; sleep 3
        done
        echo "Zookeeper ready!"
```

An initContainer runs and must complete successfully before the main container starts. This is the standard Kubernetes pattern for startup ordering.

---

### Fix 4 — Kafka JVM heap cap

**Problem:** Kafka was killed by the Linux kernel (exit code 137, OOM kill) before printing any logs. The JVM allocated as much memory as it could find on the node.

**Fix:**

```yaml
- name: KAFKA_HEAP_OPTS
  value: "-Xmx512m -Xms256m"
```

`-Xmx512m` caps the JVM heap at 512MB. Without this, the JVM defaults to a percentage of total system RAM and is killed immediately on a shared node.

---

### Fix 5 — Full cluster DNS names

**Problem:** Short service names (`zookeeper:2181`, `kafka:9092`) caused intermittent DNS failures in EKS.

**Fix:** Use fully qualified DNS names everywhere:

```yaml
- name: KAFKA_ZOOKEEPER_CONNECT
  value: zookeeper.movievault.svc.cluster.local:2181

- name: KAFKA_ADVERTISED_LISTENERS
  value: PLAINTEXT://kafka.movievault.svc.cluster.local:9092

- name: KAFKA_LISTENERS
  value: PLAINTEXT://0.0.0.0:9092
```

The pattern `<service>.<namespace>.svc.cluster.local` always resolves correctly regardless of DNS search domain configuration.

---

### Fix 6 — Fixed NodePort values

**Problem:** Random NodePort assignments on every deploy meant the frontend URLs needed updating each time.

**Fix:** Pin the NodePort in the Service spec:

```yaml
ports:
  - port: 80
    targetPort: 8080
    nodePort: 32186    # inventory — fixed forever
```

```yaml
ports:
  - port: 80
    targetPort: 8081
    nodePort: 32673    # order — fixed forever
```

---

### Fix 7 — `imagePullPolicy: Always`

**Problem:** After rebuilding Docker images with new code, pods kept running the old cached image.

**Fix:**

```yaml
image: ...ecr.../movievault/inventory-service:latest
imagePullPolicy: Always
```

Without this, Kubernetes caches the `:latest` image on the node and never re-pulls it. `Always` forces a fresh pull from ECR every time a pod starts.

---

### Fix 8 — CORS middleware in Go services

**Problem:** Browser requests from the S3 frontend failed with `Failed to fetch`. curl worked fine from the terminal.

**Root cause:** Browsers enforce CORS. The S3 site is a different origin from the NodePort API. Browsers block cross-origin requests unless the server sends `Access-Control-Allow-Origin` headers. curl is not a browser and ignores CORS entirely.

**Fix:** Add a middleware wrapper to both Go services:

```go
func corsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
        if r.Method == http.MethodOptions {
            w.WriteHeader(http.StatusOK)
            return
        }
        next.ServeHTTP(w, r)
    })
}

// wrap the mux in startServer():
log.Fatal(http.ListenAndServe(":8080", corsMiddleware(mux)))
```

The OPTIONS handler responds to browser preflight requests — the browser asks "can I call you?" before every POST/PUT/DELETE.

Verify it works:
```bash
curl -I http://<node-ip>:32186/health
# Must show: Access-Control-Allow-Origin: *
```

---

### Fix 9 — Security group inbound rules

**Problem:** External browser requests timed out even after CORS was fixed. The EC2 security group had no inbound rules for the NodePort range.

**Fix:**

```bash
NODE_SG="sg-04aef65b9f1fde894"

aws ec2 authorize-security-group-ingress \
  --group-id $NODE_SG --protocol tcp --port 32186 --cidr 0.0.0.0/0 --region us-east-1

aws ec2 authorize-security-group-ingress \
  --group-id $NODE_SG --protocol tcp --port 32673 --cidr 0.0.0.0/0 --region us-east-1
```

Find the security group ID from the node instance IDs:
```bash
aws ec2 describe-instances \
  --instance-ids i-052f20f4658df5942 i-08dd100b217876bbb \
  --query "Reservations[*].Instances[*].SecurityGroups" \
  --output table --region us-east-1
```

---

## Deployment Guide

### Prerequisites

- AWS CLI configured (`aws configure`)
- kubectl installed
- Docker running
- Go 1.22+
- An EKS cluster with EKS Auto Mode enabled

### First time setup (run once)

```bash
./scripts/01-setup-ecr.sh
```

Creates ECR repositories for both services and attaches the ECR pull policy to the node IAM role so pods can pull images.

### Every time you change Go code

```bash
./scripts/02-build-push.sh   # compile → build image → push to ECR
./scripts/03-deploy.sh       # apply manifests → rolling restart
```

### Every time you only change manifests

```bash
./scripts/03-deploy.sh       # no rebuild needed
```

### Deploy or update the frontend

```bash
./scripts/05-deploy-ui.sh
```

### Tear down (stop AWS charges)

```bash
./scripts/04-tesrdown.sh
```

Deletes the namespace and all resources. EKS Auto Mode terminates idle EC2 nodes automatically.

### Rolling restart explained

When `03-deploy.sh` runs `kubectl rollout restart`, Kubernetes replaces pods one at a time:

```
old pod 1 running    →  new pod 1 starting  →  new pod 1 passes /health
                     →  old pod 1 terminated
old pod 2 running    →  new pod 2 starting  →  new pod 2 passes /health
                     →  old pod 2 terminated
```

At no point are both old pods killed simultaneously. The service has zero downtime during deploys.

---

## Networking

### Service types

| Type | Access | Used for |
|---|---|---|
| ClusterIP | Internal only | postgres, kafka, zookeeper |
| NodePort | External via node IP + port | inventory, order services |
| LoadBalancer | External via AWS DNS (pending) | future production use |

### How NodePort works

NodePort opens the same port on every node in the cluster simultaneously. It does not matter which node receives the request — Kubernetes routes it to the correct pod internally.

```
http://18.212.19.184:32186  (node 1)
                              ↓
                        K8s routes to
                        ┌─────────────┐
                        │ inventory   │
                        │ pod 1 or 2  │
                        └─────────────┘

http://34.239.44.183:32186  (node 2) — identical result
```

### Internal DNS names

Pods communicate using these names (only resolvable from inside the cluster):

```
postgres.movievault.svc.cluster.local:5432
kafka.movievault.svc.cluster.local:9092
zookeeper.movievault.svc.cluster.local:2181
inventory-service.movievault.svc.cluster.local:80
order-service.movievault.svc.cluster.local:80
```

---

## Frontend

A single-file HTML admin panel hosted on S3 as a static website.

**S3 bucket:** `movievault-ui-cliff`
**URL:** `http://movievault-ui-cliff.s3-website-us-east-1.amazonaws.com/movievault-ui.html`

Use the `s3-website` HTTP URL. The direct S3 HTTPS URL causes Mixed Content errors because the backend APIs run over plain HTTP — browsers block an HTTPS page from calling HTTP APIs.

The UI has two input fields at the top for API URLs. Paste your NodePort URLs and click Ping. The page stores them in localStorage.

Frontend URLs (replace with your current node IP):
```
Inventory: http://<node-ip>:32186
Orders:    http://<node-ip>:32673
```

Get the current node IP:
```bash
kubectl get nodes -o wide
# look at EXTERNAL-IP column
```

---

## CORS

CORS is a browser security rule. A page on one origin (domain + port) cannot call a different origin unless the server explicitly allows it. curl and other non-browser tools do not enforce CORS.

The S3 frontend is at `http://movievault-ui-cliff.s3-website...amazonaws.com` and the APIs are at `http://18.212.19.184:32186` — these are different origins. Without CORS headers on the Go services, every browser request fails silently with `Failed to fetch`.

The fix adds `Access-Control-Allow-Origin: *` to every API response and handles OPTIONS preflight requests. The preflight is an automatic browser mechanism — before a POST or DELETE, the browser sends an OPTIONS request asking for permission. The middleware returns 200 immediately and the browser proceeds with the real request.

---

## Load Testing

### Install k6

```bash
sudo apt-get install k6
```

### Load test script (`loadtest.js`)

```javascript
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 10  },
    { duration: '30s', target: 50  },
    { duration: '30s', target: 100 },
    { duration: '30s', target: 200 },
    { duration: '30s', target: 0   },
  ],
};

const BASE = 'http://inventory-service.movievault.svc.cluster.local';

export default function () {
  const movies = http.get(`${BASE}/movies`);
  check(movies, { 'movies 200': (r) => r.status === 200 });

  const health = http.get(`${BASE}/health`);
  check(health, { 'health ok': (r) => r.status === 200 });

  sleep(0.1);
}
```

### Run from inside the cluster (accurate results)

```bash
kubectl run k6 \
  --image=grafana/k6:latest \
  --restart=Never \
  --namespace=movievault \
  --rm -i \
  --overrides='{
    "spec": {
      "dnsPolicy": "ClusterFirst",
      "tolerations": [{
        "key": "eks.amazonaws.com/compute-type",
        "operator": "Equal",
        "value": "auto",
        "effect": "NoSchedule"
      }]
    }
  }' \
  -- run - < ~/loadtest.js
```

`dnsPolicy: ClusterFirst` ensures the k6 pod uses the cluster's CoreDNS to resolve `inventory-service.movievault.svc.cluster.local`. Without it the pod uses the host DNS which has no knowledge of internal cluster names.

### Monitor during the test

```bash
watch -n 2 kubectl top pods -n movievault
watch -n 2 kubectl top nodes
```

### Reading the k6 report

| Metric | What it means |
|---|---|
| `checks_succeeded` | Percentage of requests that returned HTTP 200 |
| `http_req_duration avg` | Average time from request sent to response received |
| `p90` | 90% of requests finished faster than this |
| `p95` | 95% of requests finished faster than this — the key SLA metric |
| `max` | Worst single request — usually happens at peak load |
| `http_req_failed` | Percentage of requests that errored or timed out |
| `vus` | Virtual users — simulated concurrent browsers |
| `data_received` | Total bytes your service sent back |

### Benchmark results

| Scenario | avg | p95 | failure rate |
|---|---|---|---|
| From laptop (Nairobi to AWS) | ~299ms | ~410ms | 0.11% |
| From inside cluster | ~15ms | ~30ms | <0.01% |

The ~285ms difference between the two scenarios is almost entirely the Nairobi to us-east-1 network round trip. The Go service itself responds in approximately 3ms.

---

## Troubleshooting Reference

### Pods stuck in Pending

```bash
kubectl describe pod <pod-name> -n movievault | grep -A 15 "Events:"
```

`untolerated taint` → add the EKS Auto Mode toleration to the Deployment spec (Fix 1)

`didn't match Pod's node affinity` → stale ReplicaSets from previous failed deploys. Clean up:
```bash
kubectl delete all --all -n movievault
# then reapply manifests in order
```

### Kafka CrashLoopBackOff

```bash
kubectl logs -l app=kafka -n movievault --previous
```

Exit code 1, exits in under 2 seconds → KAFKA_PORT injection. Add `enableServiceLinks: false` (Fix 2)

Exit code 137 → OOM kill. Add `KAFKA_HEAP_OPTS: "-Xmx512m -Xms256m"` (Fix 4)

"connection refused" connecting to Zookeeper → add the initContainer (Fix 3)

### Application services CrashLoopBackOff

```bash
kubectl logs -l app=inventory-service -n movievault
```

"Kafka producer failed" → Kafka not ready yet. Wait for `kafka` pod to be `1/1 Running` then:
```bash
kubectl rollout restart deployment inventory-service order-service -n movievault
```

"DB ping failed" → postgres Secret missing. Run:
```bash
kubectl apply -f movievault/k8s/01-postgres.yaml
```

### Browser shows "Failed to fetch"

Check the following in order:

1. S3 URL uses HTTP not HTTPS: `http://movievault-ui-cliff.s3-website-us-east-1.amazonaws.com/...`
2. CORS headers present: `curl -I http://<node-ip>:32186/health` must show `Access-Control-Allow-Origin: *`
3. If curl times out → security group missing inbound rules (Fix 9)
4. If curl works but browser fails → old Docker image running without CORS. Rebuild: `./scripts/02-build-push.sh && ./scripts/03-deploy.sh`

### New Go code not taking effect

The node is using a cached old image. Force pods to pull fresh:
```bash
kubectl delete pod -l app=inventory-service -n movievault
kubectl delete pod -l app=order-service -n movievault
```

Permanently fix with `imagePullPolicy: Always` in the Deployment spec (Fix 7).

### Useful commands

```bash
# All pod statuses
kubectl get pods -n movievault

# Live CPU and RAM per pod
kubectl top pods -n movievault

# Service types and NodePorts
kubectl get svc -n movievault

# Node external IPs
kubectl get nodes -o wide

# Stream logs from a service
kubectl logs -l app=inventory-service -n movievault -f

# Connect to postgres and inspect tables
kubectl exec -it -n movievault \
  $(kubectl get pod -l app=postgres -n movievault -o jsonpath='{.items[0].metadata.name}') \
  -- psql -U appuser -d movievault

# Inside psql:
# \dt              list tables
# SELECT * FROM movies;
# SELECT * FROM orders;
# \q               quit
```