module github.com/clusternet/clusternet

go 1.14

require (
	github.com/emicklei/go-restful v2.9.5+incompatible
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/go-openapi/spec v0.19.5
	github.com/gorilla/websocket v1.4.2
	github.com/rancher/remotedialer v0.2.6-0.20210318171128-d1ebd5202be4
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	helm.sh/helm/v3 v3.6.1
	k8s.io/api v0.21.2
	k8s.io/apiextensions-apiserver v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/apiserver v0.21.2
	k8s.io/client-go v0.21.2
	k8s.io/code-generator v0.21.2
	k8s.io/component-base v0.21.2
	k8s.io/controller-manager v0.21.2
	k8s.io/klog/v2 v2.8.0
	k8s.io/kube-openapi v0.0.0-20210305001622-591a79e4bda7
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	rsc.io/letsencrypt v0.0.3 // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace (
	k8s.io/apimachinery => github.com/clusternet/apimachinery v0.21.3-rc.0.0.20210814084831-4aafc1ec60f6
	k8s.io/apiserver => github.com/clusternet/apiserver v0.21.2-0.20210722062202-17431d287b5c
)
