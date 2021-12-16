# Clusternet The Hard Way

This tutorial walks you through setting up `Clusternet` the hard way.

:white_check_mark: You can also try to [install `Clusternet` with Helm](./installing-clusternet-with-helm.md).

Clusternet The Hard Way is optimized for learning, which means taking the long route to ensure you understand each task
required to install `Clusternet`.

---

You need to deploy `clusternet-agent` in child clusters, `clusternet-hub` and `clusternet-scheduler` in parent cluster.

> :whale: :whale: :whale: Note:
>
> The container images are hosted on both [ghcr.io](https://github.com/orgs/clusternet/packages) and [dockerhub](https://hub.docker.com/u/clusternet).
> Please choose the fastest image registry to use.

## Deploying `clusternet-hub` in parent cluster

```console
kubectl apply -f deploy/hub
```

Next, you need to create a token for cluster registration, which will be used later by
`clusternet-agent`. Either a bootstrap token or a service account token is okay.

- If bootstrapping authentication is supported, i.e. `--enable-bootstrap-token-auth=true` is explicitly set in the
  kube-apiserver running in parent cluster,

  ```console
  # this will create a bootstrap token 07401b.f395accd246ae52d
  kubectl apply -f manifests/samples/cluster_bootstrap_token.yaml
  ```

- If bootstrapping authentication is not supported by the kube-apiserver in parent cluster (like [k3s](https://k3s.io/))
  , i.e. `--enable-bootstrap-token-auth=false` (which defaults to be `false`), please use serviceaccount token instead.

  ```bash
  $ # this will create a serviceaccount token
  $ kubectl apply -f manifests/samples/cluster_serviceaccount_token.yaml
  $ kubectl get secret -n clusternet-system -o=jsonpath='{.items[?(@.metadata.annotations.kubernetes\.io/service-account\.name=="cluster-bootstrap-use")].data.token}' | base64 --decode; echo
  HERE WILL OUTPUTS A LONG STRING. PLEASE REMEMBER THIS.
  ```

## Deploying `clusternet-scheduler` in parent cluster

```console
kubectl apply -f deploy/scheduler
```

### Deploying `clusternet-agent` in child cluster

`clusternet-agent` runs in child cluster and helps register self-cluster to parent cluster.

`clusternet-agent` could be configured with below three kinds of `SyncMode` (configured by flag `--cluster-sync-mode`),

- `Push` means that all the resource changes in the parent cluster will be synchronized, pushed and applied to child
  clusters by `clusternet-hub` automatically.
- `Pull` means `clusternet-agent` will watch, synchronize and apply all the resource changes from the parent cluster to
  child cluster.
- `Dual` combines both `Push` and `Pull` mode. This mode is strongly recommended, which is usually used together with
  feature gate `AppPusher`.

Feature gate `AppPusher` works on agent side, which is introduced mainly for below two reasons,

- `SyncMode` is not suggested getting changed after registration, which may bring in inconsistent settings and
  behaviors. That's why `Dual` mode is strong recommended. When `Dual` mode is set, feature gate `AppPusher` provides a
  way to help switch `Push` mode to `Pull` mode without really changing flag `--cluster-sync-mode`, and vice versa.

- For security concerns, such as child cluster security risks, etc.

  When a child cluster has disabled feature gate `AppPusher`, the parent cluster won't deploy any applications to it,
  even if SyncMode `Push` or `Dual` is set. At this time, this child cluster is working like `Pull` mode.

  Resources to be deployed are represented as `Description`, you can run your own controllers as well to watch changes
  of `Description` objects, then distribute and deploy resources.

Upon deploying `clusternet-agent`, a secret that contains token for cluster registration should be created firstly.

```bash
$ # create namespace clusternet-system if not created
$ kubectl create ns clusternet-system
$ # here we use the token created above
$ PARENTURL=https://192.168.10.10 REGTOKEN=07401b.f395accd246ae52d envsubst < ./deploy/templates/clusternet_agent_secret.yaml | kubectl apply -f -
```

> :pushpin: :pushpin: Note:
>
> If you're creating service account token above, please replace `07401b.f395accd246ae52d` with above long string
> token that outputs.

The `PARENTURL` above is the apiserver address of the parent cluster that you want to register to, the `https` scheme
must be specified and it is the only one supported at the moment. If the apiserver is not listening on the standard
https port (:443), please specify the port number in the URL to ensure the agent connects to the right endpoint, for
instance, `https://192.168.10.10:6443`.

```bash
$ # before deploying, you could update the SyncMode if needed
$ kubectl apply -f deploy/agent
```
