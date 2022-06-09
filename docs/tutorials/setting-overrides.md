# How to Set Overrides in Clusternet

`Clusternet` provides a ***two-stage priority based*** override strategy. You can define namespace-scoped `Localization`
and cluster-scoped `Globalization` with priorities (ranging from 0 to 1000, default to be 500), where lower numbers are
considered lower priority. These Globalization(s) and Localization(s) will be applied by order from lower priority to
higher. That means override values in lower `Globalization` will be overridden by those in higher `Globalization`.
Globalization(s) come first and then Localization(s).

> :dizzy: :dizzy: For example,
>
> Globalization (priority: 100) -> Globalization (priority: 600) -> Localization (priority: 100) -> Localization (priority 500)

Meanwhile, below override policies are supported,

- `ApplyNow` will apply overrides for matched objects immediately, including those are already populated.
- Default override policy `ApplyLater` will only apply override for matched objects on next updates (including updates
  on `Subscription`, `HelmChart`, etc) or new created objects.

Here you can refer below samples to learn more,

- [Localization Sample](../../examples/applications/localization.yaml)
- [Globalization Sample](../../examples/applications/globalization.yaml)

Please remember to modify the namespace to your `ManagedCluster` namespace, such as `clusternet-5l82l`.
