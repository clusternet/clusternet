# Checking Cluster Registration

- [Check Cluster Registration Request](#check-cluster-registration-request)
- [Check ManagedCluster Status](#check-managedcluster-status)

## Check Cluster Registration Request

`clusternet-agent` will automatically create a registration request `ClusterRegistrationRequest` for every cluster.

```bash
$ # clsrr is an alias for ClusterRegistrationRequest
$ kubectl get clsrr
NAME                                              CLUSTER ID                             STATUS     AGE
clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118   dc91021d-2361-4f6d-a404-7c33b9e01118   Approved   3d6h
$ kubectl get clsrr clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118 -o yaml
apiVersion: clusters.clusternet.io/v1beta1
kind: ClusterRegistrationRequest
metadata:
  labels:
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusters.clusternet.io/cluster-name: clusternet-cluster-dzqkw
    clusters.clusternet.io/registered-by: clusternet-agent
  name: clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118
spec:
  clusterId: dc91021d-2361-4f6d-a404-7c33b9e01118
  clusterName: clusternet-cluster-dzqkw
  clusterType: EdgeCluster
status:
  caCertificate: REDACTED
  dedicatedNamespace: clusternet-dhxfs
  managedClusterName: clusternet-cluster-dzqkw
  result: Approved
  token: REDACTED
```

After `ClusterRegistrationRequest` is approved, the status will be updated with corresponding credentials that can be
used to access parent cluster if needed. Those credentials have been set with scoped RBAC rules, see blow two rules for
details.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  annotations:
    clusternet.io/autoupdate: "true"
  labels:
    clusters.clusternet.io/bootstrapping: rbac-defaults
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusternet.io/created-by: clusternet-hub
  name: clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118
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
    clusternet.io/autoupdate: "true"
  labels:
    clusters.clusternet.io/bootstrapping: rbac-defaults
    clusternet.io/created-by: clusternet-hub
  name: clusternet-managedcluster-role
  namespace: clusternet-dhxfs
rules:
  - apiGroups:
      - '*'
    resources:
      - '*'
    verbs:
      - '*'
```

## Check ManagedCluster Status

For every registered cluster that gets approved, there will be a dedicated namespace created for this
cluster. `ManagedCluster` will be created as well to represent the cluster, including cluster metadata, cluster healthy
heartbeat, etc.

```bash
$ # mcls is an alias for ManagedCluster
$ # kubectl get mcls -A
$ # or append "-o wide" to display extra columns
$ kubectl get mcls -A -o wide
NAMESPACE          NAME                       CLUSTER ID                             CLUSTER TYPE   SYNC MODE   KUBERNETES   READYZ   AGE
clusternet-dhxfs   clusternet-cluster-dzqkw   dc91021d-2361-4f6d-a404-7c33b9e01118   EdgeCluster    Dual        v1.19.10     true     7d23h
$ kubectl get mcls -n clusternet-dhxfs   clusternet-cluster-dzqkw -o yaml
apiVersion: clusters.clusternet.io/v1beta1
kind: ManagedCluster
metadata:
  labels:
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusters.clusternet.io/cluster-name: clusternet-cluster-dzqkw
    clusternet.io/created-by: clusternet-agent
  name: clusternet-cluster-dzqkw
  namespace: clusternet-dhxfs
spec:
  clusterId: dc91021d-2361-4f6d-a404-7c33b9e01118
  clusterType: EdgeCluster
  syncMode: Dual
status:
  apiserverURL: http://10.0.0.10:8080
  appPusher: true
  healthz: true
  k8sVersion: v1.19.10
  lastObservedTime: "2021-06-30T08:55:14Z"
  livez: true
  platform: linux/amd64
  readyz: true
```
