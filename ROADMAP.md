# Clusternet Roadmap

This document outlines a high-level roadmap for
Clusternet. [GitHub milestones](https://github.com/clusternet/clusternet/milestones) are used to track the progress of
this project. This roadmap is updated frequently to reflect the community's overall thinking on the future of the
project.

## 2023 H1

- [ ] Cross-cluster HPA (Ongoing)
- [x] Introduces new component `clusternet-controller-manager`
- [ ] Introduces de-scheduler for Clusternet
- [ ] Enhancements on cluster resource predictor framework, such as supporting gRPC, rescheduling pending pods, etc.
- [ ] Integrations with [Node Resource Interface](https://github.com/containerd/nri/) to address cross-cluster issues, such as service discovery, dns querying, etc.
- [ ] Generic resource status aggregation
- [ ] Integration with Istio (Ongoing)
- [ ] E2E tests

## 2022

### Richer multi-cluster workload scheduling and management

- [x] Multi-cluster scheduling framework (`in-tree` plugins, `out-of-tree` plugins)
- [x] Various multi-cluster scheduling policies
  - [x] Dividing scheduling
    - [x] [Static scheduling by weight](https://clusternet.io/docs/tutorials/multi-cluster-apps/static-weight-scheduling-to-multiple-clusters/)
    - [x] [Dynamic scheduling by cluster capacity](https://clusternet.io/docs/tutorials/multi-cluster-apps/dynamic-scheduling-to-multiple-clusters/)
      - [x] Cluster resource predictor framework for `in-tree` and `out-of-tree` implementations
      - [x] Various deployment topologies for cluster resource predictors
      - [x] Resource interpretation with `in-tree` or `out-of-tree` controller
  - [x] Subgroup cluster scheduling
- [x] Integration with [mcs-api](https://github.com/kubernetes-sigs/mcs-api) to provide service discovery across multiple clusters
- [x] Multi-cluster workloads status aggregation

### Improvement of visibility and support for multi-cluster management

- [x] Cluster auto-labelling based on [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery)
- [x] Discovering clusters created by [cluster-api](https://github.com/kubernetes-sigs/cluster-api)

### Stability Improvement

- [x] Benchmarks on cluster management and application coordination

## 2021

- [x] Cluster registrations across different heterogeneous environments (e.g. public cloud, private cloud, hybrid, and
  edge cloud)
- [x] Coordination of various application types, `Deployment`, `CRD`, `HelmChart`, etc. to multiple clusters
- [x] Full RBAC supports on child clusters
- [x] Two-stage priority based override strategies
  - [x] JSON patch and Merge patch
  - [x] Easy to rollback overrides
- [x] Support for more Kubernetes versions
- [x] [Replication scheduling](https://clusternet.io/docs/tutorials/multi-cluster-apps/replication-scheduling-to-multiple-clusters/)
- [x] User and developer experience improvement
  - [x] client-go integration
  - [x] kubectl plugin
  - [x] hack scripts for building, testing, etc.
- [x] Clusternet CIs Enhancements
