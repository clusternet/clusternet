# Installing Clusternet with Helm

You need to deploy `clusternet-agent` in child clusters, `clusternet-hub` and `clusternet-scheduler` in parent cluster.

:white_check_mark: You can also try to [install `Clusternet` manually](./installing-clusternet-the-hard-way.md).

> [Helm](https://helm.sh) must be installed to use the charts. Please refer to Helm's [documentation](https://helm.sh/docs/) to get started.

Once Helm is set up properly, add the repo as follows:

```console
helm repo add clusternet https://clusternet.github.io/charts
```

You can then run `helm search repo clusternet` to see the charts.

- [installing `clusternet-hub` to parent cluster](https://github.com/clusternet/charts/tree/main/charts/clusternet-hub)
- [installing `clusternet-scheduler` to parent cluster](https://github.com/clusternet/charts/tree/main/charts/clusternet-scheduler)
- [installing `clusternet-agent` to child clusters](https://github.com/clusternet/charts/tree/main/charts/clusternet-agent)
