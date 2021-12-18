module github.com/clusternet/clusternet

go 1.14

require (
	github.com/containerd/containerd v1.4.12 // indirect
	github.com/emicklei/go-restful v2.9.5+incompatible
	github.com/evanphx/json-patch v4.11.0+incompatible
	github.com/go-openapi/spec v0.19.5
	github.com/google/go-cmp v0.5.5
	github.com/gorilla/websocket v1.4.2
	github.com/mattbaird/jsonpatch v0.0.0-20200820163806-098863c1fc24
	github.com/opencontainers/runc v1.0.3 // indirect
	github.com/rancher/remotedialer v0.2.6-0.20210318171128-d1ebd5202be4
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.2.0
	github.com/spf13/pflag v1.0.5
	helm.sh/helm/v3 v3.6.1
	k8s.io/api v0.21.2
	k8s.io/apiextensions-apiserver v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/apiserver v0.21.2
	k8s.io/client-go v0.21.2
	k8s.io/code-generator v0.21.2
	k8s.io/component-base v0.21.2
	k8s.io/component-helpers v0.21.2
	k8s.io/controller-manager v0.21.2
	k8s.io/klog/v2 v2.8.0
	k8s.io/kube-aggregator v0.21.2
	k8s.io/kube-openapi v0.0.0-20210305001622-591a79e4bda7
	k8s.io/metrics v0.21.2
	k8s.io/utils v0.0.0-20210527160623-6fdb442a123b
	rsc.io/letsencrypt v0.0.3 // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/dgrijalva/jwt-go => github.com/dgrijalva/jwt-go v3.2.1-0.20210802184156-9742bd7fca1c+incompatible
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	github.com/opencontainers/image-spec => github.com/opencontainers/image-spec v1.0.2
	github.com/spf13/afero => github.com/spf13/afero v1.5.1
	google.golang.org/grpc => google.golang.org/grpc v1.27.1
	k8s.io/apimachinery => github.com/clusternet/apimachinery v0.21.3-rc.0.0.20210814084831-4aafc1ec60f6
	k8s.io/apiserver => github.com/clusternet/apiserver v0.21.2-0.20210722062202-17431d287b5c
)
