# Visiting Managed Clusters With RBAC

- [Using curl](#using-curl)
    - [If you're using tokens](#if-youre-using-tokens)
    - [If you're using TLS certificates](#if-youre-using-tls-certificates)
- [Using KubeConfig](#using-kubeconfig)
    - [An Easy Way - kubectl-clusternet Plugin (Recommended)](#an-easy-way---kubectl-clusternet-plugin-recommended)
    - [A Hard Way - Constructing a Dedicated KubeConfig](#a-hard-way---constructing-a-dedicated-kubeconfig)
        - [Step 1: Modify Server URL](#step-1-modify-server-url)
        - [Step 2: Configure Credentials from Child Clusters](#step-2-configure-credentials-from-child-clusters)

:thumbsup: ***Clusternet supports visiting all your managed clusters with RBAC directly from parent cluster.***

Here we assume the `kube-apiserver` running in parent cluster allows **anonymous requests**. That is
flag `--anonymous-auth` (default to be `true`) is not set to `false` explicitly.

If not, an extra token from parent cluster is required.

## Using curl

Below is a simple snippet to show how to list namespaces in a child cluster with `curl`.

```bash
$ PARENTCLUSTERAUTH="Basic system:anonymous"
```

If anonymous auth is not allowed, then

```bash
$ PARENTCLUSTERTOKEN=`kubectl get secret -n clusternet-system -o=jsonpath='{.items[?(@.metadata.annotations.kubernetes\.io/service-account\.name=="clusternet-hub-proxy")].data.token}' | base64 --decode`
$ PARENTCLUSTERAUTH="Bearer ${PARENTCLUSTERTOKEN}"
```

### If you're using tokens

```bash
$ # Here the token is base64 decoded and from your child cluster. (PLEASE CHANGE ME!!!)
$ CHILDCLUSTERTOKEN="TOKEN-BASE64-DECODED-IN-YOUR-CHILD-CLUSTER"
$ # specify the child cluster id (PLEASE CHANGE ME!!!)
$ CHILDCLUSTERID="dc91021d-2361-4f6d-a404-7c33b9e01118"
$ # The Parent Cluster APIServer Address (PLEASE CHANGE ME!!!)
$ APISERVER="https://10.0.0.10:6443"
$ curl -k -XGET  -H "Accept: application/json" \
  -H "Impersonate-User: clusternet" \
  -H "Authorization: ${PARENTCLUSTERAUTH}" \
  -H "Impersonate-Extra-Clusternet-Token: ${CHILDCLUSTERTOKEN}" \
  "${APISERVER}/apis/proxies.clusternet.io/v1alpha1/sockets/${CHILDCLUSTERID}/proxy/direct/api/v1/namespaces"
```

### If you're using TLS certificates

```bash
$ # base64 encoded certificate from your child cluster. (PLEASE CHANGE ME!!!)
$ CHILDCLUSTERCERT="CERTIFICATE-BASE64-ENCODED-IN-YOUR-CHILD-CLUSTER"
$ # base64 encoded privatekey from your child cluster. (PLEASE CHANGE ME!!!)
$ CHILDCLUSTERKEY="PRIVATEKEY-BASE64-ENCODED-IN-YOUR-CHILD-CLUSTER"
$ # specify the child cluster id (PLEASE CHANGE ME!!!)
$ CHILDCLUSTERID="dc91021d-2361-4f6d-a404-7c33b9e01118"
$ # The Parent Cluster APIServer Address (PLEASE CHANGE ME!!!)
$ APISERVER="https://10.0.0.10:6443"
$ curl -k -XGET  -H "Accept: application/json" \
  -H "Impersonate-User: clusternet" \
  -H "Authorization: ${PARENTCLUSTERAUTH}" \
  -H "Impersonate-Extra-Clusternet-Certificate: ${CHILDCLUSTERCERT}" \
  -H "Impersonate-Extra-Clusternet-PrivateKey: ${CHILDCLUSTERKEY}" \
  "${APISERVER}/apis/proxies.clusternet.io/v1alpha1/sockets/${CHILDCLUSTERID}/proxy/direct/api/v1/namespaces"
```

### Using KubeConfig

### An Easy Way - kubectl-clusternet Plugin (Recommended)

First,
please [install/upgrade `kubectl-clusternet` plugin](https://github.com/clusternet/kubectl-clusternet#installation) with
a minimum required version `v0.5.0`.

```bash
$ kubectl clusternet version
{
  "gitVersion": "0.5.0",
  "platform": "darwin/amd64"
}
```

```bash
$ kubectl get mcls -A
NAMESPACE          NAME       CLUSTER ID                             SYNC MODE   KUBERNETES                   READYZ   AGE
clusternet-ml6wg   aws-cd     6c085c18-3baf-443c-abff-459751f5e3d3   Dual        v1.18.4                      true     4d6h
clusternet-z5vqv   azure-cd   7dc5966e-6736-48dd-9a82-2e4d74d30443   Dual        v1.20.4                      true     43h
$ kubectl clusternet --cluster-id=7dc5966e-6736-48dd-9a82-2e4d74d30443 --child-kubeconfig=./azure-cd-kubeconfig get ns
NAME                STATUS   AGE
clusternet-system   Active   4d20h
default             Active   24d
kube-node-lease     Active   24d
kube-public         Active   24d
kube-system         Active   24d
test-nginx          Active   11d
test-systemd        Active   11d
```

Here the apisever in above kubeconfig file `azure-cd-kubeconfig` could be with an inner address. Now you can easily
check any objects status in child clusters.

### A Hard Way - Constructing a Dedicated KubeConfig

You need to follow below **2 steps** to construct a dedicated kubeconfig to access a child cluster with `kubectl`.

#### Step 1: Modify Server URL

Append `/apis/proxies.clusternet.io/v1alpha1/sockets/<CLUSTER-ID>/proxy/https/<SERVER-URL>`
or `/apis/proxies.clusternet.io/v1alpha1/sockets/<CLUSTER-ID>/proxy/direct` at the end of original **parent cluster**
server address

> - `CLUSTER-ID` is a UUID for your child cluster, which is auto-populated by `clusternet-agent`, such as dc91021d-2361-4f6d-a404-7c33b9e01118. You could get this UUID from objects `ClusterRegistrationRequest`,
    `ManagedCluster`, etc. Also this UUID is labeled with key `clusters.clusternet.io/cluster-id`.
>
>- `SERVER-URL` is the apiserver address of your child cluster, it could be `localhost`, `127.0.0.1` and etc, only if
   `clusternet-agent` could access.

You can follow below commands to help modify above changes.

```bash
$ # suppose your parent cluster kubeconfig locates at /home/demo/.kube/config.parent
$ kubectl config view --kubeconfig=/home/demo/.kube/config.parent --minify=true --raw=true > ./config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
$
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
$
$ # suppose your child cluster running at https://demo1.cluster.net
$ kubectl config set-cluster `kubectl config get-clusters | grep -v NAME` \
  --server=https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/https/demo1.cluster.net
$ # or just use the direct proxy path
$ kubectl config set-cluster `kubectl config get-clusters | grep -v NAME` \
  --server=https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/direct
```

> :pushpin: :pushpin: Note:
>
> Clusternet supports both http and https scheme.
>
> If you want to use scheme `http` to demonstrate how it works, i.e. `/apis/proxies.clusternet.io/v1alpha1/sockets/<CLUSTER-ID>/proxy/http/<SERVER-URL>`,
> you can simply ***run a local proxy in your child cluster***, for example,
>
> ```bash
   > $ kubectl proxy --address='10.212.0.7' --accept-hosts='^*$'
   > ```
>
> Please replace `10.212.0.7` with your real local IP address.
>
> Then follow above url modification as well.

#### Step 2: Configure Credentials from Child Clusters

Then update user entry with **credentials from child clusters**

> :see_no_evil: :see_no_evil: Note:
>
> `Clusternet-hub` does not care about those credentials at all, passing them directly to child clusters.


<details>
  <summary>:white_check_mark: If You're Using Tokens (Please Click to Expand!)</summary>

Here the tokens can be [bootstrap tokens](https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/),
[ServiceAccount tokens](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#use-multiple-service-accounts)
, etc.

Please follow below modifications.

```bash
$ export KUBECONFIG=`pwd`/config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
$ # below is what we modified in above step 1
$ kubectl config view
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: DATA+OMITTED
    server: https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/direct
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
$
$ # modify user part to below
$ vim config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
  ...
  user:
    username: system:anonymous
    as: clusternet
    as-user-extra:
        clusternet-token:
            - BASE64-DECODED-PLEASE-CHANGE-ME
```

Please replace `BASE64-DECODED-PLEASE-CHANGE-ME` to a token that valid from **child cluster**. ***Please notice the
tokens replaced here should be base64 decoded.***

> :pushpin: :pushpin: Important Note:
>
> If anonymous auth is not allowed, please replace `username: system:anonymous` to `token: PARENT-CLUSTER-TOKEN`.
> Here `PARENT-CLUSTER-TOKEN` can be retrieved with below command,
>
>```bash
>kubectl get secret -n clusternet-system -o=jsonpath='{.items[?(@.metadata.annotations.kubernetes\.io/service-account\.name=="clusternet-hub-proxy")].data.token}' | base64 --decode; echo
>```

</details>

<details>
  <summary>:white_check_mark: If You're Using TLS Certificates (Please Click to Expand!)</summary>

Please follow below modifications.

```bash
$ export KUBECONFIG=`pwd`/config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
$ # below is what we modified in above step 1
$ kubectl config view
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: DATA+OMITTED
    server: https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/direct
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
$
$ # modify user part to below
$ vim config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
  ...
  user:
    username: system:anonymous
    as: clusternet
    as-user-extra:
        clusternet-certificate:
            - CLIENT-CERTIFICATE-DATE-BASE64-ENCODED-PLEASE-CHANGE-ME
        clusternet-privatekey:
            - CLIENT-KEY-DATE-PLEASE-BASE64-ENCODED-CHANGE-ME
```

Please replace `CLIENT-CERTIFICATE-DATE-BASE64-ENCODED-PLEASE-CHANGE-ME`
and `CLIENT-KEY-DATE-PLEASE-BASE64-ENCODED-CHANGE-ME` with certficate and private key from child cluster. **Please
notice the tokens replaced here should be base64 encoded.**

> :pushpin: :pushpin: Important Note:
>
> If anonymous auth is not allowed, please replace `username: system:anonymous` to `token: PARENT-CLUSTER-TOKEN`.
> Here `PARENT-CLUSTER-TOKEN` can be retrieved with below command,
>
>```bash
>kubectl get secret -n clusternet-system -o=jsonpath='{.items[?(@.metadata.annotations.kubernetes\.io/service-account\.name=="clusternet-hub-proxy")].data.token}' | base64 --decode; echo
>```

</details>
