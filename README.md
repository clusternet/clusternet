<div><img src="https://clusternet.io/images/clusternet-horizontal-color.png" style="width:300px;" /></div>

[![GoPkg Widget](https://pkg.go.dev/badge/github.com/clusternet/clusternet.svg)](https://pkg.go.dev/github.com/clusternet/clusternet)
[![License](https://img.shields.io/github/license/clusternet/clusternet)](https://www.apache.org/licenses/LICENSE-2.0.html)
![GoVersion](https://img.shields.io/github/go-mod/go-version/clusternet/clusternet)
[![OpenSSF Best Practices](https://bestpractices.coreinfrastructure.org/projects/7185/badge)](https://bestpractices.coreinfrastructure.org/projects/7185)
[![Go Report Card](https://goreportcard.com/badge/github.com/clusternet/clusternet)](https://goreportcard.com/report/github.com/clusternet/clusternet)
![build](https://github.com/clusternet/clusternet/actions/workflows/ci.yml/badge.svg)
[![Version](https://img.shields.io/github/v/release/clusternet/clusternet)](https://github.com/clusternet/clusternet/releases)
[![codecov](https://codecov.io/gh/clusternet/clusternet/branch/main/graph/badge.svg)](https://codecov.io/gh/clusternet/clusternet)
[![FOSSA Status](https://app.fossa.com/api/projects/custom%2B162%2Fgithub.com%2Fclusternet%2Fclusternet.svg?type=shield&issueType=license)](https://app.fossa.com/projects/custom%2B162%2Fgithub.com%2Fclusternet%2Fclusternet?ref=badge_shield)

----

Managing Your Clusters (including public, private, hybrid, edge, etc.) as easily as Visiting the Internet.

Out of the Box.

A CNCF([Cloud Native Computing Foundation](https://cncf.io/)) Sandbox Project.

----

<div align="center"><img src="https://clusternet.io/images/clusternet-in-a-nutshell.png" style="width:900px;" /></div>

Clusternet (**Cluster** Inter**net**) is an open source ***add-on*** that helps you manage thousands of millions of
Kubernetes clusters as easily as visiting the Internet. No matter the clusters are running on public cloud, private
cloud, hybrid cloud, or at the edge, Clusternet helps setup network tunnels in a configurable way and lets you
manage/visit them all as if they were running locally. This also help eliminate the need to juggle different management
tools for each cluster.

**Clusternet can also help deploy and coordinate applications to multiple clusters from a single set of APIs in a
hosting cluster.**

Clusternet also provides a Kubernetes-styled API, where you can continue using the Kubernetes way, such as KubeConfig,
to visit a certain Managed Kubernetes cluster, or a Kubernetes service.

Clusternet is multiple platforms supported now, including `linux/amd64`, `linux/arm64`, `linux/ppc64le`, `linux/s390x`
, `linux/386` and `linux/arm`;

----

## Core Features

- Kubernetes Multi-Cluster Management and Governance
    - managing Kubernetes clusters running in cloud providers, such as AWS, Google Cloud, Tencent Cloud, Alibaba Cloud,
      etc.
    - managing on-premise Kubernetes clusters
    - managing any [Certified Kubernetes Distributions](https://www.cncf.io/certification/software-conformance/), such
      as [k3s](https://github.com/k3s-io/k3s)
    - managing Kubernetes clusters running at the edge
    - automatically discovering and registering clusters created by [cluster-api](https://github.com/kubernetes-sigs/cluster-api)
    - parent cluster can also register itself as a child cluster to run workloads
    - managing Kubernetes upper than v1.17.x (Learn more
      about [Kubernetes Version Skew](https://clusternet.io/docs/introduction/#kubernetes-version-skew))
    - visiting any managed clusters with dynamic RBAC rules (Learn more
      from [this tuorial](https://clusternet.io/docs/tutorials/cluster-management/visiting-child-clusters-with-rbac/))
    - cluster auto-labelling based on [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery)
- Application Coordinations
    - Scheduling **Framework** (`in-tree` plugins, `out-of-tree` plugins)
    - Cross-Cluster Scheduling
        - replication scheduling
        - static dividing scheduling by weight
        - dynamic dividing scheduling by capacity
          - cluster resource predictor **framework** for `in-tree` and `out-of-tree` implementations
          - various deployment topologies for cluster resource predictors
        - subgroup cluster scheduling
    - Various Resource Types
        - Kubernetes native objects, such as `Deployment`, `StatefulSet`, etc.
        - CRDs
        - helm charts, including [OCI-based Helm charts](https://helm.sh/docs/topics/registries/)
    - Resource interpretations with `in-tree` or `out-of-tree` controllers
    - [Setting Overrides](https://clusternet.io/docs/tutorials/multi-cluster-apps/setting-overrides/)
        - two-stage priority based override strategies
        - easy to rollback overrides
        - cross-cluster canary rollout
    - Multi-Cluster Services
        - multi-cluster services discovery with [mcs-api](https://github.com/kubernetes-sigs/mcs-api)
- CLI
    - providing a kubectl plugin, which can be installed with `kubectl krew install clusternet`
    - consistent user experience with `kubectl`
    - create/update/watch/delete multi-cluster resources
    - interacting with any child clusters the same as local cluster
- Client-go
    - easy to integrate via
      a [client-go wrapper](https://github.com/clusternet/clusternet/blob/main/examples/clientgo/READEME.md)

## Architecture

![](https://clusternet.io/images/clusternet-arch.png)

Clusternet is a lightweight addon that consists of four components, `clusternet-agent`, `clusternet-scheduler`,
`clusternet-controller-manager` and `clusternet-hub`.

Explore the architecture of Clusternet on [clusternet.io](https://clusternet.io/docs/introduction/#architecture).

## To start using Clusternet

See our documentation on [clusternet.io](https://clusternet.io/docs/).

The [quick start tutorial](https://clusternet.io/docs/quick-start/) will walk you through setting up Clusternet locally
with [kind](https://kind.sigs.k8s.io/) and deploying applications to multiple clusters.

Try our [interactive tutorials](https://clusternet.io/docs/tutorials/) that help you understand Clusternet and learn
some basic Clusternet features.

If you want to use [client-go](https://github.com/kubernetes/client-go) to interact with Clusternet, we provide a
wrapper for easy integration. You can
follow [demo.go](https://github.com/clusternet/clusternet/blob/main/examples/clientgo/demo.go) for a quick start.

To use Clusternet APIs and CRDs as a module, please add [github.com/clusternet/apis](https://github.com/clusternet/apis)
to your `go.mod`.

## Contact

If you've got any questions, please feel free to contact us with following ways:

- [open a github issue](https://github.com/clusternet/clusternet/issues/new/choose)
- [mailing list](mailto:clusternet@googlegroups.com)
- [join discussion group](https://groups.google.com/g/clusternet)

## Contributing & Developing

If you want to get participated and become a contributor to Clusternet, please don't hesitate to refer to our
[CONTRIBUTING](CONTRIBUTING.md) document for details.

A [developer guide](https://clusternet.io/docs/developer-guide/) is ready to help you

- build binaries for all platforms, such as `darwin/amd64`, `linux/amd64`, `linux/arm64`, etc.;
- build docker images for multiple platforms, such as `linux/amd64`, `linux/arm64`, etc.;

---

<div align="center"><img src="https://raw.githubusercontent.com/cncf/artwork/master/other/cncf/horizontal/color/cncf-color.svg" style="width:600px;" /></div>


## License
[![FOSSA Status](https://app.fossa.com/api/projects/custom%2B162%2Fgithub.com%2Fclusternet%2Fclusternet.svg?type=large)](https://app.fossa.com/projects/custom%2B162%2Fgithub.com%2Fclusternet%2Fclusternet?ref=badge_large)
