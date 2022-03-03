# Deploying Applications to Multiple Clusters

Clusternet supports deploying applications to multiple clusters from a single set of APIs in a hosting cluster.

> :pushpin: :pushpin: Note:
>
> Feature gate `Deployer` should be enabled by `clusternet-hub`.

- [Defining Your Applications](#defining-your-applications)
- [Setting Overrides](#setting-overrides)
- [Applying Your Applications](#applying-your-applications)
- [Checking Status](#checking-status)

## Defining Your Applications

First, let's see an exmaple application. Below `Subscription` "app-demo" defines the target child clusters to be
distributed to, and the resources to be deployed with.

```yaml
# examples/applications/subscription.yaml
apiVersion: apps.clusternet.io/v1alpha1
kind: Subscription
metadata:
  name: app-demo
  namespace: default
spec:
  subscribers: # defines the clusters to be distributed to
    - clusterAffinity:
        matchLabels:
          clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118 # PLEASE UPDATE THIS CLUSTER-ID TO YOURS!!!
  feeds: # defines all the resources to be deployed with
    - apiVersion: apps.clusternet.io/v1alpha1
      kind: HelmChart
      name: mysql
      namespace: default
    - apiVersion: v1
      kind: Namespace
      name: foo
    - apiVersion: apps/v1
      kind: Service
      name: my-nginx-svc
      namespace: foo
    - apiVersion: apps/v1
      kind: Deployment
      name: my-nginx
      namespace: foo
```

Before applying this `Subscription`, please
modify [examples/applications/subscription.yaml](https://github.com/clusternet/clusternet/blob/main/examples/applications/subscription.yaml)
with your clusterID.

> :bulb: :bulb:
> If you want to install a helm chart from a private helm repository, please set a valid `chartPullSecret` by referring
> [this example](../../deploy/templates/helm-chart-private-repo.yaml).

Clusternet also supports using [OCI-based registries](https://helm.sh/docs/topics/registries/) for Helm charts. Please
refer [this oci-based helm chart](../../examples/oci/oci-chart-mysql.yaml).

## Setting Overrides

`Clusternet` also provides a ***two-stage priority based*** override strategy. You can define
namespace-scoped `Localization` and cluster-scoped `Globalization` with priorities (ranging from 0 to 1000, default to
be 500), where lower numbers are considered lower priority. These Globalization(s) and Localization(s) will be applied
by order from lower priority to higher. That means override values in lower `Globalization` will be overridden by those
in higher `Globalization`. Globalization(s) come first and then Localization(s).

> :dizzy: :dizzy: For example,
>
> Globalization (priority: 100) -> Globalization (priority: 600) -> Localization (priority: 100) -> Localization (priority 500)

Meanwhile, below override policies are supported,

- `ApplyNow` will apply overrides for matched objects immediately, including those are already populated.
- Default override policy `ApplyLater` will only apply override for matched objects on next updates (including updates
  on `Subscription`, `HelmChart`, etc) or new created objects.

Before applying these Localization(s), please
modify [examples/applications/localization.yaml](https://github.com/clusternet/clusternet/blob/main/examples/applications/localization.yaml)
with your `ManagedCluster` namespace, such as `clusternet-5l82l`.

## Applying Your Applications

After installing kubectl plugin [kubectl-clusternet](https://github.com/clusternet/kubectl-clusternet), you could run
below commands to distribute this application to child clusters.

```bash
$ kubectl clusternet apply -f examples/applications/
helmchart.apps.clusternet.io/mysql created
namespace/foo created
deployment.apps/my-nginx created
service/my-nginx-svc created
subscription.apps.clusternet.io/app-demo created
$ # or
$ # kubectl-clusternet apply -f examples/applications/
```

## Checking Status

Then you can view the resources just created,

```bash
$ # list Subscription
$ kubectl clusternet get subs -A
NAMESPACE   NAME       AGE
default     app-demo   6m4s
$ kubectl clusternet get chart
NAME             CHART   VERSION   REPO                                 STATUS   AGE
mysql            mysql   8.6.2     https://charts.bitnami.com/bitnami   Found    71s
$ kubectl clusternet get ns
NAME   CREATED AT
foo    2021-08-07T08:50:55Z
$ kubectl clusternet get svc -n foo
NAME           CREATED AT
my-nginx-svc   2021-08-07T08:50:57Z
$ kubectl clusternet get deploy -n foo
NAME       CREATED AT
my-nginx   2021-08-07T08:50:56Z
```

`Clusternet` will help deploy and coordinate applications to multiple clusters. You can check the status by following
commands,

```bash
$ kubectl clusternet get mcls -A
NAMESPACE          NAME                       CLUSTER ID                             SYNC MODE   KUBERNETES   READYZ   AGE
clusternet-5l82l   clusternet-cluster-hx455   dc91021d-2361-4f6d-a404-7c33b9e01118   Dual        v1.21.0      true     5d22h
$ # list Descriptions
$ kubectl clusternet get desc -A
NAMESPACE          NAME               DEPLOYER   STATUS    AGE
clusternet-5l82l   app-demo-generic   Generic    Success   2m55s
clusternet-5l82l   app-demo-helm      Helm       Success   2m55s
$ kubectl describe desc -n clusternet-5l82l   app-demo-generic
...
Status:
  Phase:  Success
Events:
  Type    Reason                Age    From            Message
  ----    ------                ----   ----            -------
  Normal  SuccessfullyDeployed  2m55s  clusternet-hub  Description clusternet-5l82l/app-demo-generic is deployed successfully
$ # list Helm Release
$ # hr is an alias for HelmRelease
$ kubectl clusternet get hr -n clusternet-5l82l
NAME                  CHART       VERSION   REPO                                 STATUS     AGE
helm-demo-mysql       mysql       8.6.2     https://charts.bitnami.com/bitnami   deployed   2m55s
```

You can also verify the installation with Helm command line in your child cluster,

```bash
$ helm ls -n abc
NAME               	NAMESPACE	REVISION	UPDATED                             	STATUS  	CHART            	APP VERSION
helm-demo-mysql    	abc      	1       	2021-07-06 14:34:44.188938 +0800 CST	deployed	mysql-8.6.2      	8.0.25
```

> :pushpin: :pushpin: Note:
>
> Admission webhooks could be configured in parent cluster, but please make sure that [dry-run](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#side-effects) mode is supported in these webhooks. At the same time, a webhook must explicitly indicate that it will not have side-effects when running with `dryRun`. That is [`sideEffects`](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#side-effects) must be set to `None` or `NoneOnDryRun`.
>
> While, these webhooks could be configured per child cluster without above limitations as well.
