# Using Client-Go in Clusternet

If you want to use [client-go](https://github.com/kubernetes/client-go) to interact with Clusternet, you only need to
insert below `wrapperFunc` in your codes, while the rest remains the same.

```go
// This is the ONLY place you need to wrap for Clusternet
config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
    return clientgo.NewClusternetTransport(config.Host, rt)
})
```

You can follow [demo.go](./demo.go) for a quick start.
