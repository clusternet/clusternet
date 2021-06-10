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
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/rancher/remotedialer"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/proxy"
	genericfeatures "k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/registry/rest"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"

	proxies "github.com/clusternet/clusternet/pkg/apis/proxies/v1alpha1"
)

type Exchanger struct {
	// use cachedTransports to speed up networking connections
	cachedTransports map[string]*http.Transport

	lock sync.Mutex

	// dialerServer is used for serving websocket connection
	dialerServer *remotedialer.Server
}

var (
	urlPrefix = fmt.Sprintf("/apis/%s/sockets/", proxies.SchemeGroupVersion.String())
)

func authorizer(req *http.Request) (string, bool, error) {
	vars := mux.Vars(req)
	clusterID := vars["cluster"]
	return clusterID, clusterID != "", nil
}

func NewExchanger(tunnelLogging bool) *Exchanger {
	if tunnelLogging {
		logrus.SetLevel(logrus.DebugLevel)
		remotedialer.PrintTunnelData = true
	}

	e := &Exchanger{
		cachedTransports: map[string]*http.Transport{},
		dialerServer:     remotedialer.New(authorizer, remotedialer.DefaultErrorWriter),
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
	// serve websocket connection
	router := mux.NewRouter().PathPrefix(fmt.Sprintf("%s{cluster}", urlPrefix)).Subrouter()
	router.Handle("", e.dialerServer)
	router.Handle("/", e.dialerServer)

	// TODO: add metrics

	// proxy and re-dial through websocket connection
	router.HandleFunc("/direct{path:.*}",
		func(writer http.ResponseWriter, request *http.Request) {
			location, transport, err := e.ClusterLocation(true, true, request, id)
			if err != nil {
				responder.Error(err)
				return
			}

			klog.V(4).Infof("Request to %q will be redialed from cluster %q", location.String(), id)
			handler := newThrottledUpgradeAwareProxyHandler(location, transport, false, false, true, true, responder)
			handler.ServeHTTP(writer, request)
		})
	router.HandleFunc("/{scheme}/{host}{path:.*}",
		func(writer http.ResponseWriter, request *http.Request) {
			location, transport, err := e.ClusterLocation(false, true, request, id)
			if err != nil {
				responder.Error(err)
				return
			}

			klog.V(4).Infof("Request to %q will be redialed from cluster %q", location.String(), id)
			handler := newThrottledUpgradeAwareProxyHandler(location, transport, false, false, true, true, responder)
			handler.ServeHTTP(writer, request)
		})
	return router, nil
}

func (e *Exchanger) ClusterLocation(isShortPath, useSocket bool, request *http.Request, id string) (*url.URL, http.RoundTripper, error) {
	location := new(url.URL)
	var transport *http.Transport

	vars := mux.Vars(request)
	location.Path = vars["path"]
	location.RawQuery = request.URL.RawQuery

	if isShortPath {
		// TODO

	} else {
		location.Scheme = vars["scheme"]
		location.Host = vars["host"]
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
