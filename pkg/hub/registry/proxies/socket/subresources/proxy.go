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

package subresources

import (
	"context"
	"fmt"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"

	proxiesapi "github.com/clusternet/clusternet/pkg/apis/proxies/v1alpha1"
	"github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/hub/exchanger"
)

// ProxyREST implements the proxy subresource for a Socket
type ProxyREST struct {
	Exchanger           *exchanger.Exchanger
	socketConnection    bool
	ExtraHeaderPrefixes []string
}

// Implement Connecter
var _ = rest.Connecter(&ProxyREST{})
var _ = rest.Storage(&ProxyREST{})

var proxyMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// New returns an empty podProxyOptions object.
func (r *ProxyREST) New() runtime.Object {
	return &proxiesapi.Socket{}
}

func (r *ProxyREST) Destroy() {
}

// ConnectMethods returns the list of HTTP methods that can be proxied
func (r *ProxyREST) ConnectMethods() []string {
	return proxyMethods
}

// NewConnectOptions returns versioned resource that represents proxy parameters
func (r *ProxyREST) NewConnectOptions() (runtime.Object, bool, string) {
	return &proxiesapi.Socket{}, true, "path"
}

// Connect returns a handler for the pod proxy
func (r *ProxyREST) Connect(ctx context.Context, id string, opts runtime.Object, responder rest.Responder) (http.Handler, error) {
	if !r.socketConnection {
		return nil, apierrors.NewServiceUnavailable(fmt.Sprintf("featuregate %s has not been enabled on the server side", features.SocketConnection))
	}

	proxyOpts, ok := opts.(*proxiesapi.Socket)
	if !ok {
		return nil, fmt.Errorf("invalid options object: %#v", opts)
	}

	return r.Exchanger.ProxyConnect(ctx, id, proxyOpts, responder, r.ExtraHeaderPrefixes)
}

// NewProxyREST returns a RESTStorage object that will work against API services.
func NewProxyREST(socketConnection bool, ec *exchanger.Exchanger, extraHeaderPrefixes []string) *ProxyREST {
	return &ProxyREST{
		Exchanger:           ec,
		socketConnection:    socketConnection,
		ExtraHeaderPrefixes: extraHeaderPrefixes,
	}
}
