# Setting Up Your Development Environment Locally

This tutorial walks you through setting up `Clusternet` locally with 1 parent cluster and 3 child clusters by using 
[kind](https://kind.sigs.k8s.io/).

## Prerequisites
- [Helm](https://helm.sh/) version v3.8.0
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) version v1.23.4
- [kind](https://kind.sigs.k8s.io/) version v0.11.1
- [Docker](https://docs.docker.com/) version v20.10.2

## Clone Clusternet

Clone the repository,

```bash
mkdir -p $GOPATH/src/github.com/clusternet/
cd $GOPATH/src/github.com/clusternet/
git clone https://github.com/clusternet/clusternet
cd clusternet
```

## Deploy and run clusternet:

Run the following script,

```bash
hack/local-running.sh
```

If everything goes well, you will see the messages as follows:

```
Local clusternet is running now.
To start using clusternet, please run:
  export KUBECONFIG="${HOME}/.kube/clusternet.config"
  kubectl config get-contexts
```

When you run `kubectl config get-contexts`, you will see 1 parent cluster and 3 child clusters and the clusternet has
been deployed automatically.

```bash
# kubectl config get-contexts
CURRENT   NAME     CLUSTER       AUTHINFO      NAMESPACE
          child1   kind-child1   kind-child1   
          child2   kind-child2   kind-child2   
          child3   kind-child3   kind-child3   
*         parent   kind-parent   kind-parent
# kubectl get pod -n clusternet-system 
NAME                                    READY   STATUS    RESTARTS   AGE
clusternet-hub-7d4bf55fbd-9lv9h         1/1     Running   0          3m2s
clusternet-scheduler-8645f9d85b-cdlr5   1/1     Running   0          2m59s
clusternet-scheduler-8645f9d85b-fmfln   1/1     Running   0          2m59s
clusternet-scheduler-8645f9d85b-vkw8r   1/1     Running   0          2m59s
```
