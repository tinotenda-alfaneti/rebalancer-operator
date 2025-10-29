# Rebalancer Operator

The Rebalancer Operator observes node and workload pressure and performs targeted evictions to nudge the Kubernetes scheduler towards a balanced cluster. It focuses on safety, transparency, and configurability.

## Architecture

- **Custom Resources**
  - `RebalancePolicy` (cluster-scoped) controls thresholds, hysteresis, rate limits, and namespace/workload selection.
  - `WorkloadSLO` (namespaced) guards disruptions by enforcing disruption budgets, readiness windows, and stateful allowances.
  - `NodeClass` (cluster-scoped) groups nodes and influences scoring.
- **Planner**
  - Fetches pods, nodes, PDBs, PriorityClasses, and policy resources using the controller-runtime client and typed informers.
  - Pulls utilization from metrics-server and optional Prometheus queries.
  - Scores nodes and workloads with hysteresis, variance reduction (`Δσ`), taint/affinity checks, budgets, and backoff windows.
- **Executor**
  - Issues graceful evictions through the Kubernetes eviction subresource when policies and budgets permit.
- **Observability**
  - Prometheus metrics: node utilization gauges, variance (`rebalancer_sigma`), eviction counters, dry-run counters, and namespace budget gauges.
  - Sample Grafana dashboard under `dashboards/rebalancer-overview.json`.

## Key Features

- Utilization scoring combines CPU and memory with configurable weights.
- Hysteresis avoids churn between hot/cold classification.
- Namespace/policy budgets, WorkloadSLO max-disruptions-per-hour, and per-pod backoff timers reduce disruption risk.
- Safety predicates skip DaemonSets, annotated/pinned pods, workloads using large local storage, and high-priority/system-critical pods.
- Optional Prometheus integration for custom node/pod metrics.
- Helm chart (`charts/rebalancer`) to deploy the operator and an optional default policy.

## Running the Operator

```sh
kubectl apply -f deploy/crds/
kubectl apply -f deploy/samples/rebalancepolicy_default.yaml
kubectl apply -f deploy/samples/workloadslo_prod_api.yaml
kubectl apply -f deploy/samples/nodeclass_rpi4.yaml

# or via Helm
helm install rebalancer charts/rebalancer --create-namespace --namespace rebalancer-system
```

Set `samples.workloadSLO.enabled=true` and/or `samples.nodeClass.enabled=true` to have Helm install the bundled guardrail resources instead of applying the sample manifests manually. Their fields (namespace, labels, weight, etc.) are exposed under the same `samples.*` keys in `values.yaml`.

The operator defaults to dry-run mode. Flip `spec.dryRun` to `false` in the `RebalancePolicy` to enable real evictions. The `policy.selectorLabel` ensures only opt-in workloads (`rebalancer=ok`) are eligible.

## Metrics

Metric | Type | Labels | Description
------ | ---- | ------ | -----------
`rebalancer_node_utilization` | Gauge | `node`, `resource` | Latest CPU/memory utilization ratios per node.
`rebalancer_sigma` | Gauge | – | Cluster utilization variance (lower is better).
`rebalancer_evictions_total` | Counter | `namespace`, `reason` | Successful evictions executed by the operator.
`rebalancer_dry_run_evictions_total` | Counter | – | Evictions that would occur when dry-run is enabled.
`rebalancer_budget_remaining` | Gauge | `namespace` | Remaining disruption budget units for namespaces with limits (`-1` = unlimited).

Scrape the built-in metrics endpoint on port 8080 (see Helm values for customisation).

## Development Workflow

```sh
# Build
go build ./...

# Lint / static analysis (requires golangci-lint)
golangci-lint run ./...

# Unit tests
go test ./...

# Run locally against a kubeconfig
go run ./cmd/manager \
  --metrics-bind-address=:8080 \
  --health-probe-bind-address=:8081 \
  --prometheus-address=http://prometheus-operated.monitoring.svc:9090

# Deploy using Helm with real evictions disabled
helm upgrade --install rebalancer charts/rebalancer \ 
  --namespace rebalancer-system --create-namespace \ 
  --set policy.dryRun=true
```

## Testing Playbook

1. **Dry-run smoke test** – apply the default policy, confirm `rebalancer_dry_run_evictions_total` increases while no pods are evicted.
2. **Budget enforcement** – create a `WorkloadSLO` with `maxDisruptionsPerHour: 1`, trigger two hot pods, ensure the second eviction remains pending with `reason` containing `budget exhausted`.
3. **PDB compliance** – attach a `PodDisruptionBudget` limiting disruptions; verify the operator skips pods when `DisruptionsAllowed == 0`.
4. **Node class preference** – define `NodeClass` for ARM nodes, mark workloads as CPU-bound, and observe reduced variance and destination bias via logs/metrics.
5. **Priority guardrails** – mark a pod `system-cluster-critical` and confirm it is filtered from candidates.

## Optional UI

The operator exposes Prometheus metrics that can power a lightweight dashboard. A starter Grafana board is bundled; a simple Svelte/React application can call the metrics endpoint or custom APIs to visualise planned moves and budgets.

## Container Image & CI/CD

The repository includes a multi-stage `Dockerfile` that emits a statically linked `rebalancer` binary and runs it from a `distroless` base. Build it locally (for example, targeting ARM64 for Raspberry Pi nodes) with:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -t tinorodney/rebalancer:dev .
```

Automated builds and deployments are orchestrated via the provided `Jenkinsfile`. The pipeline:

1. Installs `kubectl`, `helm`, and Go on the Jenkins worker.
2. Runs `go test ./...`.
3. Provisions namespaces, secrets, and RBAC required for Kaniko on the MicroK8s cluster.
4. Launches a Kaniko job (`ci/kubernetes/kaniko.yaml`) to produce a multi-arch image pushed to Docker Hub.
5. Executes a Trivy scan (`ci/kubernetes/trivy.yaml`) against the freshly built image.
6. Deploys or upgrades the operator with the bundled Helm chart (`charts/rebalancer`).
7. Runs Helm tests and reports results.

Both Kaniko and Trivy job templates accept substitution of `__CONTEXT_URL__`, `__CONTEXT_SUBDIR__`, and `__IMAGE_DEST__` so the Jenkinsfile can inject the correct Git context and image tag. Ensure the cluster has a `dockerhub-creds` secret and the `kaniko-builder` service account with cluster-admin binding before running the pipeline.

## Dashboard Access

The Helm values enable an ingress by default at `https://rebalancer.atarnet.org/`. If you prefer a different host or controller, adjust `ingress.hosts` and `ingress.className` in `charts/rebalancer/values.yaml`. The UI is also reachable via port forwarding on the metrics port (`kubectl port-forward deploy/rebalancer 8080:8080`) and visiting `http://localhost:8080/dashboard`.
