# Rebalancer Operator

This repository contains a Kubernetes operator that monitors node and pod pressure and evicts eligible workloads to balance utilisation across the cluster. It complements, rather than replaces, the default scheduler.

- Custom resources: `RebalancePolicy`, `WorkloadSLO`, `NodeClass`
- Planner with metrics-server and optional Prometheus integration
- Safety rails for PDBs, priorities, affinities, storage, budgets, and per-pod backoff
- Prometheus metrics and sample Grafana dashboard
- Helm chart for quick installation

See [`docs/README.md`](docs/README.md) for detailed architecture, metrics, and testing guidance.
