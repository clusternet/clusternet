# Deploying Applications to Multiple Clusters with Static Weight Scheduling

Clusternet supports deploying applications to multiple clusters from a single set of APIs in a hosting cluster.

> :pushpin: :pushpin: Note:
>
> Feature gate `Deployer` should be enabled by `clusternet-hub`.

This tutorial will walk you through how to deploy applications to multiple clusters with static weight scheduling. It is
different from replication scheduling. When using static weight scheduling, the replicas of an application will be split
based on cluster weights. For example, if you want to deploy a `Deployment` with 6 replicas to 2 clusters ("cluster-01"
with weight 1, "cluster-02" with weight 2), then "cluster-01" will run such a `Deployment` with 2 replicas, "cluster-02"
runs the other 4 replicas.

- [Defining Your Applications](#defining-your-applications)
- [Applying Your Applications](#applying-your-applications)

## Defining Your Applications

Let's see an example using static weight scheduling. Below `Subscription` "static-dividing-scheduling-demo" defines the
target child clusters to be distributed to, and the resources to be deployed with.

```yaml
# examples/static-dividing-scheduling/subscription.yaml
apiVersion: apps.clusternet.io/v1alpha1
kind: Subscription
metadata:
  name: static-dividing-scheduling-demo
  namespace: default
spec:
  subscribers: # defines the clusters to be distributed to
    - clusterAffinity:
        matchLabels:
          clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118 # PLEASE UPDATE THIS CLUSTER-ID TO YOURS!!!
      weight: 1 # Deployment bar/my-nginx will have 2 replicas running in this cluster
    - clusterAffinity:
        matchLabels:
          clusters.clusternet.io/cluster-id: 5f9da921-0437-4fea-a89d-42aa1ede9b25 # PLEASE UPDATE THIS CLUSTER-ID TO YOURS!!!
      weight: 2 # Deployment bar/my-nginx will have 4 replicas running in this cluster
  schedulingStrategy: Dividing
  dividingScheduling:
    type: Static
  feeds: # defines all the resources to be deployed with
    - apiVersion: v1
      kind: Namespace
      name: bar
    - apiVersion: v1
      kind: Service
      name: my-nginx-svc
      namespace: bar
    - apiVersion: apps/v1 # with a total of 6 replicas
      kind: Deployment
      name: my-nginx
      namespace: bar
```

The `Deployment` bar/my-nginx above will run in two clusters with a total of 6 replicas, while 2 replicas run in cluster
with ID `dc91021d-2361-4f6d-a404-7c33b9e01118`, 4 replicas in cluster with ID `5f9da921-0437-4fea-a89d-42aa1ede9b25`.

Before applying this `Subscription`, please
modify [examples/static-dividing-scheduling/subscription.yaml](https://github.com/clusternet/clusternet/blob/main/examples/static-dividing-scheduling/subscription.yaml)
with your clusterID.

If you want to apply overrides per cluster, please follow [How to Set Overrides in Clusternet](./setting-overrides.md).

## Applying Your Applications

After installing kubectl plugin [kubectl-clusternet](https://github.com/clusternet/kubectl-clusternet), you could run
commands below to distribute this application to child clusters.

```bash
$ kubectl clusternet apply -f examples/static-dividing-scheduling/
namespace/bar created
deployment.apps/my-nginx created
service/my-nginx-svc created
subscription.apps.clusternet.io/static-dividing-scheduling-demo created
$ # or
$ # kubectl-clusternet apply -f examples/static-dividing-scheduling/
```
