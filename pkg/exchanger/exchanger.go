/*
Copyright 2021 The Clusternet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package exchanger

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/rancher/remotedialer"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/proxy"
	genericfeatures "k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/registry/rest"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	proxies "github.com/clusternet/clusternet/pkg/apis/proxies/v1alpha1"
	clusterInformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/clusters/v1beta1"
	clusterListers "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
)

type Exchanger struct {
	// use cachedTransports to speed up networking connections
	cachedTransports map[string]*http.Transport

	lock sync.Mutex

	// dialerServer is used for serving websocket connection
	dialerServer *remotedialer.Server

	mcLister clusterListers.ManagedClusterLister
	mcSynced cache.InformerSynced
}

var (
	urlPrefix = fmt.Sprintf("/apis/%s/sockets/", proxies.SchemeGroupVersion.String())
)

func authorizer(req *http.Request) (string, bool, error) {
	clusterID := strings.TrimPrefix(strings.TrimRight(req.URL.Path, "/"), urlPrefix)
	return clusterID, clusterID != "", nil
}

func NewExchanger(tunnelLogging bool, mclsInformer clusterInformers.ManagedClusterInformer) *Exchanger {
	if tunnelLogging {
		logrus.SetLevel(logrus.DebugLevel)
		remotedialer.PrintTunnelData = true
	}

	e := &Exchanger{
		cachedTransports: map[string]*http.Transport{},
		dialerServer:     remotedialer.New(authorizer, remotedialer.DefaultErrorWriter),
		mcLister:         mclsInformer.Lister(),
		mcSynced:         mclsInformer.Informer().HasSynced,
	}
	return e
}

func (e *Exchanger) getClonedTransport(clusterID string) *http.Transport {
	// return cloned transport to avoid being changed outside
	e.lock.Lock()
	defer e.lock.Unlock()

	transport := e.cachedTransports[clusterID]
	if transport != nil {
		return transport.Clone()
	}

	dialer := e.dialerServer.Dialer(clusterID)
	transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DialContext: dialer,
		// apply default settings from http.DefaultTransport
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	e.cachedTransports[clusterID] = transport
	return transport.Clone()
}

func (e *Exchanger) Connect(ctx context.Context, id string, opts *proxies.Socket, responder rest.Responder) (http.Handler, error) {
	return e.dialerServer, nil
}

func (e *Exchanger) ProxyConnect(ctx context.Context, id string, opts *proxies.Socket, responder rest.Responder) (http.Handler, error) {
	// Wait for the caches to be synced before starting workers
	if !cache.WaitForCacheSync(ctx.Done(), e.mcSynced) {
		err := apierrors.NewServiceUnavailable("cache for ManagedCluster is not ready yet, please retry later")
		responder.Error(err)
		return nil, err
	}

	location, transport, err := e.ClusterLocation(true, id, opts)
	if err != nil {
		responder.Error(err)
		return nil, err
	}

	// TODO: add metrics

	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		location.RawQuery = request.URL.RawQuery
		// proxy and re-dial through websocket connection
		klog.V(4).Infof("Request to %q will be redialed from cluster %q", location.String(), id)
		handler := newThrottledUpgradeAwareProxyHandler(location, transport, false, false, true, true, responder)
		handler.ServeHTTP(writer, request)
	})

	return handler, nil
}

func (e *Exchanger) ClusterLocation(useSocket bool, id string, opts *proxies.Socket) (*url.URL, http.RoundTripper, error) {
	var transport *http.Transport

	location := new(url.URL)

	reqPath := strings.TrimLeft(opts.Path, "/")
	var isShortPath bool
	if len(reqPath) == 0 {
		isShortPath = true
	}

	if isShortPath {
		mcls, err := e.mcLister.List(labels.SelectorFromSet(labels.Set{
			known.ClusterIDLabel: id,
		}))
		if err != nil {
			return nil, nil, apierrors.NewServiceUnavailable(err.Error())
		}
		if len(mcls) > 1 {
			klog.Warningf("found multiple ManagedCluster dedicated for cluster %s !!!", id)
		}

		apiserverURL := mcls[0].Status.APIServerURL
		if len(apiserverURL) == 0 {
			return nil, nil, apierrors.NewServiceUnavailable(fmt.Sprintf("cannot retrieve valid apiserver url for cluster %s", id))
		}

		loc, err := url.Parse(apiserverURL)
		if err != nil {
			return nil, nil, apierrors.NewInternalError(err)
		}

		location.Scheme = loc.Scheme
		location.Host = loc.Host
		location.Path = loc.Path
	} else {
		parts := strings.Split(reqPath, "/")
		if len(parts) == 0 {
			return nil, nil, fmt.Errorf("unexpected error: invalid request path %s", reqPath)
		}
		location.Scheme = parts[0]
		if len(parts) > 1 {
			location.Host = parts[1]
		}
		if len(parts) > 2 {
			location.Path = path.Join(parts[2:]...)
		}
	}

	// TODO: support https as well
	if location.Scheme != "http" {
		return nil, nil, apierrors.NewBadRequest(fmt.Sprintf("scheme %s is not supported now, please use http", location.Scheme))
	}

	if useSocket {
		if !e.dialerServer.HasSession(id) {
			return nil, nil, apierrors.NewBadRequest(fmt.Sprintf("cannot proxy through cluster %s, whose agent is disconnected", id))
		}
		transport = e.getClonedTransport(id)
	}

	return location, transport, nil
}

func newThrottledUpgradeAwareProxyHandler(location *url.URL, transport http.RoundTripper, wrapTransport, upgradeRequired, interceptRedirects, useLocationHost bool, responder rest.Responder) *proxy.UpgradeAwareHandler {
	// if location.Path is empty, a status code 301 will be returned by below handler with a new location
	// ends with a '/'. This is essentially a hack for http://issue.k8s.io/4958.
	handler := proxy.NewUpgradeAwareHandler(location, transport, wrapTransport, upgradeRequired, proxy.NewErrorResponder(responder))
	handler.InterceptRedirects = interceptRedirects && utilfeature.DefaultFeatureGate.Enabled(genericfeatures.StreamingProxyRedirects)
	handler.RequireSameHostRedirects = utilfeature.DefaultFeatureGate.Enabled(genericfeatures.ValidateProxyRedirects)
	handler.UseLocationHost = useLocationHost
	return handler
}
