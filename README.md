# Clusternet

----

Managing Your Clusters (including public, private, hybrid, edge, etc) as easily as Visiting the Internet.

----

Clusternet (**Cluster** Inter**net**) is an open source ***add-on*** that helps you manage thousands of millions of
Kubernetes clusters as easily as visiting the Internet. No matter the clusters are running on public cloud,
private cloud, hybrid cloud, or at the edge, Clusternet lets you manage/visit them all as if they were running locally.
This also help eliminate the need to juggle different management tools for each cluster.

Clusternet will help setup network tunnels in a configurable way, when your clusters are running in a VPC
network, at the edge, or behind a firewall.

Clusternet also provides a Kubernetes-styled API, where you can continue using the Kubernetes way, such
as KubeConfig, to visit a certain Managed Kubernetes cluster, or a Kubernetes service.

Clusternet is multiple platforms supported now, including

- `darwin/amd64` and `darwin/arm64`;
- `linux/amd64`, `linux/arm64`, `linux/ppc64le`, `linux/s390x`, `linux/386` and `linux/arm`;

# Architecture

Clusternet is light-weighted that consists of two components, `clusternet-agent` and `clusternet-hub`.

`clusternet-agent` is responsible for
- auto-registering current cluster to a parent cluster as a child cluster, which is also been called `ManagedCluster`;
- reporting heartbeats of current cluster, including Kubernetes version, running platform, `healthz`/`readyz`/`livez` status, etc;
- setting up a websocket connection that provides full-duplex communication channels over a single TCP connection
  to parent cluster;

`clusternet-hub` is responsible for
- approving cluster registration requests and creating exclusive resources, such as namespaces, serviceaccounts and
  RBAC rules, for each child cluster;
- serving as an **aggregated apiserver (AA)**, which is used to serve as a websocket server that maintain multiple active
  websocket connections from child clusters;
- providing Kubernstes-styled API to redirect/proxy/upgrade requests to each child cluster;

> Note: Since `clusternet-hub` is running as an AA, please make sure that parent apiserver could visit the
> `clusternet-hub` service.

# Concepts

For every Kubernetes cluster that wants to be managed, we call it **child cluster**. The cluster where
child clusters are registerring to, we call it **parent cluster**.

`clusternet-agent` runs in child cluster, while `clusternet-hub` runs in parent cluster.

- `ClusterRegistrationRequest` is an object that `clusternet-agent` creates in parent cluster for child cluster registration.
- `ManagedCluster` is an object that `clusternet-hub` creates in parent cluster after approving `ClusterRegistrationRequest`.

# Building

## Building Binaries

Clone the repository, and run

```bash
# build for linux/amd64 by default
$ make clusternet-agent clusternet-hub
```

to build binaries `clusternet-agent` and `clusternet-hub` for `linux/amd64`.

Also you could specify other platforms when building, such as,

```bash
# build only clusternet-agent for linux/arm64 and darwin/amd64
# use comma to separate multiple platforms
$ PLATFORMS=linux/arm64,darwin/amd64 make clusternet-agent
# below are all the supported platforms
# PLATFORMS=darwin/amd64,darwin/arm64,linux/amd64,linux/arm64,linux/ppc64le,linux/s390x,linux/386,linux/arm
```

All the built binaries will be placed at `_output` folder.

## Building Docker Images

You can also build docker images. Here `docker buildx` is used to help build multi-arch container images.

If you're running MacOS, please install [Docker Desktop](https://docs.docker.com/desktop/) and then check the builder,

```bash
$ docker buildx ls
NAME/NODE DRIVER/ENDPOINT STATUS  PLATFORMS
default * docker
  default default         running linux/amd64, linux/arm64, linux/ppc64le, linux/s390x, linux/386, linux/arm/v7, linux/arm/v6
```

If you're running Linux, please refer to [docker buildx docs](https://docs.docker.com/buildx/working-with-buildx/)
on the installation.

```bash
# build for linux/amd64 by default
# container images for clusternet-agent and clusternet-hub
$ make images
```

Also you could build container images for other platforms, such as `arm64`,

```bash
$ PLATFORMS=linux/amd64,linux/arm64,linux/ppc64le make images
# below are all the supported platforms
# PLATFORMS=linux/amd64,linux/arm64,linux/ppc64le,linux/s390x,linux/386,linux/arm
```

# Getting Started

## Deploy Clusternet

You need to deploy `clusternet-agent` and `clusternet-hub` in child cluster and parent cluster
respectively.

### For `clusternet-hub`

```bash
kubectl apply -f deploy/hub
```

And then create a bootstrap token for `clusternet-agent`,

```bash
# this will create a bootstrap token 07401b.f395accd246ae52d
$ kubectl apply -f manifests/samples
```

### For `clusternet-agent`

First we need to create a secret, which contains token for cluster registration,

```bash
# create namespace edge-system if not created
$ kubectl create ns edge-system
# here we use the token created above
$ PARENTURL=https://192.168.10.10 REGTOKEN=07401b.f395accd246ae52d envsubst < ./deploy/templates/clusternet_agent_secret.yaml | kubectl apply -f -
```

The `PARENTURL` above is the apiserver address of the parent cluster that you want to register to.

```bash
$ kubectl apply -f deploy/agent
```

## Check Cluster Registrations

```bash
# clsrr is an alias for ClusterRegistrationRequest
$ kubectl get clsrr
NAME                                              CLUSTER-ID                             STATUS     AGE
clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118   dc91021d-2361-4f6d-a404-7c33b9e01118   Approved   3d6h
$ kubectl get clsrr clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118 -o yaml
apiVersion: clusters.clusternet.io/v1beta1
kind: ClusterRegistrationRequest
metadata:
  creationTimestamp: "2021-05-24T08:24:40Z"
  generation: 1
  labels:
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusters.clusternet.io/cluster-name: clusternet-cluster-dzqkw
    clusters.clusternet.io/registered-by: clusternet-agent
  name: clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118
  resourceVersion: "553624"
  uid: 8531ee8a-c66a-439e-bb5a-3adacfe58952
spec:
  clusterId: dc91021d-2361-4f6d-a404-7c33b9e01118
  clusterName: clusternet-cluster-dzqkw
  clusterType: EdgeClusterSelfProvisioned
status:
  caCertificate: REDACTED
  dedicatedNamespace: clusternet-dhxfs
  managedClusterName: clusternet-cluster-dzqkw
  result: Approved
  token: REDACTED
```

After `ClusterRegistrationRequest` is approved, the status will be updated with corresponding credentials that can
be used to access parent cluster if needed. Those credentials have been set with scoped RBAC rules, see blow two rules
for details.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  annotations:
    clusters.clusternet.io/rbac-autoupdate: "true"
  creationTimestamp: "2021-05-24T08:25:07Z"
  labels:
    clusters.clusternet.io/bootstrapping: rbac-defaults
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusternet.io/created-by: clusternet-hub
  name: clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118
  resourceVersion: "553619"
  uid: 87db2e72-f4c1-4628-9373-1536ed7fd4af
rules:
  - apiGroups:
      - clusters.clusternet.io
    resources:
      - clusterregistrationrequests
    verbs:
      - create
      - get
  - apiGroups:
      - proxies.clusternet.io
    resourceNames:
      - dc91021d-2361-4f6d-a404-7c33b9e01118
    resources:
      - sockets
    verbs:
      - '*'
```

and

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  annotations:
    clusters.clusternet.io/rbac-autoupdate: "true"
  creationTimestamp: "2021-05-24T08:25:07Z"
  labels:
    clusters.clusternet.io/bootstrapping: rbac-defaults
    clusternet.io/created-by: clusternet-hub
  name: clusternet-managedcluster-role
  namespace: clusternet-dhxfs
  resourceVersion: "553622"
  uid: 7524b743-57f3-4a45-a6cd-ceb3321fe2ff
rules:
  - apiGroups:
      - '*'
    resources:
      - '*'
    verbs:
      - '*'
```

### Check ManagedCluster Status

```bash
# mcls is an alias for ManagedCluster
$ kubectl get mcls -A
NAMESPACE          NAME                       CLUSTER-ID                             CLUSTER-TYPE                 KUBERNETES   READYZ   AGE
clusternet-dhxfs   clusternet-cluster-dzqkw   dc91021d-2361-4f6d-a404-7c33b9e01118   EdgeClusterSelfProvisioned   v1.19.10     true     2d20h
$ kubectl get mcls -n clusternet-dhxfs   clusternet-cluster-dzqkw -o yaml
apiVersion: clusters.clusternet.io/v1beta1
kind: ManagedCluster
metadata:
  creationTimestamp: "2021-05-24T08:25:07Z"
  generation: 1
  labels:
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusters.clusternet.io/cluster-name: clusternet-cluster-dzqkw
    clusternet.io/created-by: clusternet-agent
  name: clusternet-cluster-dzqkw
  namespace: clusternet-dhxfs
  resourceVersion: "555091"
  uid: e7e7fb5f-1a00-4e4e-aa02-2e943e37e4ff
spec:
  clusterId: dc91021d-2361-4f6d-a404-7c33b9e01118
  clusterType: EdgeClusterSelfProvisioned
status:
  healthz: true
  k8sVersion: v1.19.10
  lastObservedTime: "2021-05-24T08:58:30Z"
  livez: true
  platform: linux/amd64
  readyz: true
```

The status of `ManagedCluster` is updated by `clusternet-agent` every 3 minutes for default, which can be configured by
flag `--cluster-status-update-frequency`.

### Visit ManagedCluster

You can visit all your managed clusters using the kubeConfig of parent cluster.
Only a small modification is needed.

```bash
# suppose your parent cluster kubeconfig locates at /home/demo/.kube/config
$ kubectl config view --kubeconfig=/home/demo/.kube/config --minify=true --raw=true > ./config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
$ export KUBECONFIG=`pwd`/config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
$ kubectl config view
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: DATA+OMITTED
    server: https://10.0.0.10:6443
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: kubernetes-admin
  name: kubernetes-admin@kubernetes
current-context: kubernetes-admin@kubernetes
kind: Config
preferences: {}
users:
- name: kubernetes-admin
  user:
    client-certificate-data: REDACTED
    client-key-data: REDACTED
# suppose child cluster running at http://demo1.cluster.net
$ kubectl config set-cluster `kubectl config get-clusters | grep -v NAME` \
  --server=https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/http/demo1.cluster.net
```

What you need is to append `/apis/proxies.clusternet.io/v1alpha1/sockets/<CLUSTER-ID>/http/<SERVER-URL>` at the end
of original parent cluster server address.
- `CLUSTER-ID` is a UUID for your child cluster, which is auto-populated by `clusternet-agent`, such as
  dc91021d-2361-4f6d-a404-7c33b9e01118. You could get this UUID from objects `ClusterRegistrationRequest`,
  `ManagedCluster`, etc. Also this UUID is labeled with key `clusters.clusternet.io/cluster-id`.
- `SERVER-URL` is the apiserver address of your child cluster, it could be `localhost`, `127.0.0.1` and etc, only if
  `clusternet-agent` could access.

Currently Clusternet only support http scheme. If your child clusters are running with https scheme, you could run a
local proxy instead, for example,

```bash
kubectl proxy --address='10.212.0.7' --accept-hosts='^*$'
```

Please replace `10.212.0.7` with your real local IP address.

Then you can visit child cluster as usual.
